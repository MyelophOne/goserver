package goserver

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/sync/singleflight"
)

type SessionId string
type Session map[string]any

const (
	sessionCookieName = "session_id"
	sessionExpiration = 43_200
)

var settingsSession = struct {
	Storage    SessionStorage
	CookieName string
	Expiration time.Duration
}{
	CookieName: sessionCookieName,
	Expiration: time.Duration(sessionExpiration) * time.Second,
}

type SessionStorage interface {
	Get(id string) (Session, bool)
	Set(id string, data Session) error
	Delete(id string) error
	GetOrSetSession(id string, generate func() Session) (Session, error)
	GetOrSet(id string, key string, generate func() (any, error)) (any, error)
}

type InMemorySessionStorage struct {
	group singleflight.Group
	cache *lru.Cache
	mu    sync.RWMutex
}

type memItem struct {
	Data      Session
	ExpiresAt time.Time
}

func NewInMemorySessionStorage(size int) *InMemorySessionStorage {
	cache, _ := lru.New(size)
	return &InMemorySessionStorage{cache: cache}
}

func (s *InMemorySessionStorage) Get(id string) (Session, bool) {
	val, ok := s.cache.Get(id)
	if !ok {
		return nil, false
	}
	item := val.(memItem)
	if time.Now().After(item.ExpiresAt) {
		s.cache.Remove(id)
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	dataCopy := make(Session)
	for k, v := range item.Data {
		dataCopy[k] = v
	}
	return dataCopy, true
}

func (s *InMemorySessionStorage) Set(id string, data Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dataCopy := make(Session)
	for k, v := range data {
		dataCopy[k] = v
	}
	s.cache.Add(id, memItem{
		Data:      dataCopy,
		ExpiresAt: time.Now().Add(settingsSession.Expiration),
	})
	return nil
}

func (s *InMemorySessionStorage) Delete(id string) error {
	s.cache.Remove(id)
	return nil
}

func (s *Server) SetSessionStorage(storage SessionStorage) {
	if storage != nil {
		settingsSession.Storage = storage
	}
}

func GenerateSessionID() SessionId {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return SessionId(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return SessionId(fmt.Sprintf("%x", b))
}

func (s *Server) SessionCookie(w *http.ResponseWriter, r *http.Request) SessionId {
	cookie, err := s.GetCookie(r, settingsSession.CookieName)
	if err != nil || cookie == nil || cookie.Value == "" {
		id := GenerateSessionID()
		isSecure := true
		if AppEnv == "dev" {
			isSecure = false
		}
		s.SetCookie(*w, settingsSession.CookieName, string(id), CookieOptions{
			MaxAge:   int(settingsSession.Expiration.Seconds()),
			Secure:   isSecure,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		return id
	}
	return SessionId(cookie.Value)
}

func (s *Server) SessionStart(w *http.ResponseWriter, r *http.Request) SessionId {
	id := s.SessionCookie(w, r)

	if settingsSession.Storage == nil {
		s.Logger.Printf("FATAL: Session storage is not initialized. Cannot start session %s.", id)
		return id
	}

	_, err := settingsSession.Storage.GetOrSetSession(string(id), func() Session {
		return make(Session)
	})

	if err != nil {
		s.Logger.Printf("ERROR: Failed to init session %s: %v", id, err)
	}

	return id
}

func (id SessionId) SetSessionValue(name string, value any) {
	if settingsSession.Storage == nil {
		fmt.Print("[goserver] error: settingsSession.Storage is nil (SetSessionValue)")
		return
	}
	data, ok := settingsSession.Storage.Get(string(id))
	if !ok {
		data = make(Session)
	}
	data[name] = value
	_ = settingsSession.Storage.Set(string(id), data)
}

func (id SessionId) GetSessionValue(name string) any {
	data, ok := settingsSession.Storage.Get(string(id))
	if !ok {
		return nil
	}
	return data[name]
}

func (id SessionId) GetAllSessionValues() Session {
	data, ok := settingsSession.Storage.Get(string(id))
	if !ok {
		return make(Session)
	}
	return data
}

func (id SessionId) Destroy(w *http.ResponseWriter) {
	_ = settingsSession.Storage.Delete(string(id))
	RemoveCookie(*w, settingsSession.CookieName)
}

func (s *InMemorySessionStorage) GetOrSetSession(id string, generate func() Session) (Session, error) {
	if val, ok := s.Get(id); ok {
		return val, nil
	}

	val, err, _ := s.group.Do(id, func() (any, error) {
		if val, ok := s.Get(id); ok {
			return val, nil
		}

		newData := generate()

		err := s.Set(id, newData)
		if err != nil {
			return nil, err
		}

		return newData, nil
	})

	if err != nil {
		return nil, err
	}
	return val.(Session), nil
}

func (s *InMemorySessionStorage) GetOrSet(id string, key string, generate func() (any, error)) (any, error) {
	s.mu.RLock()
	valRaw, ok := s.cache.Get(id)
	s.mu.RUnlock()

	if ok {
		item := valRaw.(memItem)
		if val, exists := item.Data[key]; exists {
			return val, nil
		}
	}

	sfKey := fmt.Sprintf("sess_val:%s:%s", id, key)

	result, err, _ := s.group.Do(sfKey, func() (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()

		valRaw, ok := s.cache.Get(id)
		var sessionData Session

		if !ok {
			sessionData = make(Session)
		} else {
			item := valRaw.(memItem)
			if time.Now().After(item.ExpiresAt) {
				s.cache.Remove(id)
				sessionData = make(Session)
			} else {
				sessionData = item.Data
			}
		}

		if val, exists := sessionData[key]; exists {
			return val, nil
		}

		s.mu.Unlock()
		newValue, genErr := generate()
		s.mu.Lock()

		if genErr != nil {
			return nil, genErr
		}

		if valRawAfter, okAfter := s.cache.Get(id); okAfter {
			item := valRawAfter.(memItem)
			item.Data[key] = newValue
		} else {
			newData := make(Session)
			newData[key] = newValue
			s.cache.Add(id, memItem{
				Data:      newData,
				ExpiresAt: time.Now().Add(settingsSession.Expiration),
			})
		}

		return newValue, nil
	})

	return result, err
}

func GetOrSetSessionValue[T any](
	id SessionId,
	key string,
	generate func() (T, error),
) (T, error) {
	genWrapper := func() (any, error) {
		return generate()
	}

	val, err := settingsSession.Storage.GetOrSet(string(id), key, genWrapper)
	var zero T

	if err != nil {
		return zero, err
	}

	if v, ok := val.(T); ok {
		return v, nil
	}

	bytes, _ := json.Marshal(val)
	if err := json.Unmarshal(bytes, &zero); err != nil {
		return zero, fmt.Errorf("session: failed to convert value to type %T: %w", zero, err)
	}

	return zero, nil
}
