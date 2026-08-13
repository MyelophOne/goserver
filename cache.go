package goserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/sync/singleflight"
)

type CacheStore interface {
	GetOrSetSWR(ctx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) ([]byte, error)
	Get(ctx context.Context, key string) (any, bool)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	GetOrSet(ctx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) ([]byte, error)
}

type HybridCache struct {
	group       singleflight.Group
	memoryCache *lru.Cache
	dataDir     string
}

type cacheItem struct {
	Value      []byte
	Expiration int64
}

func NewCache(memorySize int, dataDir string) *HybridCache {
	if memorySize <= 0 {
		memorySize = 10000
	}

	memCache, err := lru.New(memorySize)
	if err != nil {
		panic(err)
	}

	c := &HybridCache{
		memoryCache: memCache,
		dataDir:     dataDir,
	}

	go c.startMemoryCleanupLoop()

	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Fatalf("failed to create cache dir: %v", err)
		}
		go c.startCleanupLoop()
	}

	return c
}

func (c *HybridCache) getSafePath(key string) string {
	hash := sha256.Sum256([]byte(key))
	hashStr := hex.EncodeToString(hash[:])
	return filepath.Join(c.dataDir, hashStr[:2], hashStr)
}

func (c *HybridCache) save(key string, data []byte, maxAge time.Duration) {
	expiration := time.Now().Add(maxAge).UnixNano()
	c.memoryCache.Add(key, cacheItem{Value: data, Expiration: expiration})

	if c.dataDir != "" {
		filePath := c.getSafePath(key)
		dir := filepath.Dir(filePath)
		_ = os.MkdirAll(dir, 0755)

		tmpPath := filePath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err == nil {
			_ = os.Rename(tmpPath, filePath)
		}
	}
}

func (c *HybridCache) triggerRevalidate(originalCtx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) {
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		_, _, _ = c.group.Do(key, func() (any, error) {
			newData, err := generate(bgCtx)
			if err != nil {
				log.Printf("[SWR Cache] Background update failed for '%s': %v", key, err)
				return nil, err
			}
			if len(newData) > 0 {
				c.save(key, newData, maxAge)
				log.Printf("[SWR Cache] Successfully updated '%s' in background", key)
			}
			return nil, nil
		})
	}()
}

func (c *HybridCache) GetOrSetSWR(ctx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) ([]byte, error) {
	now := time.Now().UnixNano()

	if val, found := c.memoryCache.Get(key); found {
		item := val.(cacheItem)
		if now <= item.Expiration {
			return item.Value, nil
		}
		c.triggerRevalidate(ctx, key, maxAge, generate)
		return item.Value, nil
	}

	if c.dataDir != "" {
		filePath := c.getSafePath(key)
		if info, err := os.Stat(filePath); err == nil {
			if data, readErr := os.ReadFile(filePath); readErr == nil {
				fileExpiration := info.ModTime().Add(maxAge).UnixNano()
				c.memoryCache.Add(key, cacheItem{Value: data, Expiration: fileExpiration})

				if now <= fileExpiration {
					return data, nil
				}
				c.triggerRevalidate(ctx, key, maxAge, generate)
				return data, nil
			}
		}
	}

	v, err, _ := c.group.Do(key, func() (any, error) {
		if val, found := c.memoryCache.Get(key); found {
			item := val.(cacheItem)
			if now <= item.Expiration {
				return item.Value, nil
			}
		}

		newData, genErr := generate(ctx)
		if genErr != nil {
			return nil, genErr
		}
		if len(newData) == 0 {
			return nil, fmt.Errorf("generator returned empty data")
		}

		c.save(key, newData, maxAge)
		return newData, nil
	})

	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func (c *HybridCache) GetOrSet(ctx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) ([]byte, error) {
	now := time.Now().UnixNano()

	if val, found := c.memoryCache.Get(key); found {
		item := val.(cacheItem)
		if now <= item.Expiration {
			return item.Value, nil
		}
		c.memoryCache.Remove(key)
	}

	if c.dataDir != "" {
		filePath := c.getSafePath(key)
		if info, err := os.Stat(filePath); err == nil {
			if data, readErr := os.ReadFile(filePath); readErr == nil {
				fileExpiration := info.ModTime().Add(maxAge).UnixNano()

				if now <= fileExpiration {
					c.memoryCache.Add(key, cacheItem{Value: data, Expiration: fileExpiration})
					return data, nil
				}
			}
		}
	}

	v, err, _ := c.group.Do(key, func() (any, error) {
		newData, genErr := generate(ctx)
		if genErr != nil {
			return nil, genErr
		}
		if len(newData) == 0 {
			return nil, fmt.Errorf("generator returned empty data")
		}

		c.save(key, newData, maxAge)
		return newData, nil
	})

	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func Fetch[T any](ctx context.Context, store CacheStore, key string, maxAge time.Duration, generate func(ctx context.Context) (T, error)) (T, error) {
	var zero T

	byteGenerator := func(genCtx context.Context) ([]byte, error) {
		result, err := generate(genCtx)
		if err != nil {
			return nil, err
		}
		if b, ok := any(result).([]byte); ok {
			return b, nil
		}
		if s, ok := any(result).(string); ok {
			return []byte(s), nil
		}
		return json.Marshal(result)
	}

	cachedBytes, err := store.GetOrSet(ctx, key, maxAge, byteGenerator)
	if err != nil {
		return zero, err
	}

	if _, isBytes := any(zero).([]byte); isBytes {
		return any(cachedBytes).(T), nil
	}
	if _, isString := any(zero).(string); isString {
		return any(string(cachedBytes)).(T), nil
	}

	var finalResult T
	if err := json.Unmarshal(cachedBytes, &finalResult); err != nil {
		return zero, fmt.Errorf("cache unmarshal error: %w", err)
	}

	return finalResult, nil
}

func (c *HybridCache) startCleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		maxAgeLimit := 14 * 24 * time.Hour
		now := time.Now()
		_ = filepath.Walk(c.dataDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".tmp") || now.Sub(info.ModTime()) > maxAgeLimit {
				_ = os.Remove(path)
			}
			return nil
		})
	}
}

func FetchSWR[T any](ctx context.Context, store CacheStore, key string, maxAge time.Duration, generate func(ctx context.Context) (T, error)) (T, error) {
	var zero T

	byteGenerator := func(genCtx context.Context) ([]byte, error) {
		result, err := generate(genCtx)
		if err != nil {
			return nil, err
		}
		if b, ok := any(result).([]byte); ok {
			return b, nil
		}
		if s, ok := any(result).(string); ok {
			return []byte(s), nil
		}
		return json.Marshal(result)
	}

	cachedBytes, err := store.GetOrSetSWR(ctx, key, maxAge, byteGenerator)
	if err != nil {
		return zero, err
	}

	if _, isBytes := any(zero).([]byte); isBytes {
		return any(cachedBytes).(T), nil
	}
	if _, isString := any(zero).(string); isString {
		return any(string(cachedBytes)).(T), nil
	}

	var finalResult T
	if err := json.Unmarshal(cachedBytes, &finalResult); err != nil {
		return zero, fmt.Errorf("cache unmarshal error: %w", err)
	}

	return finalResult, nil
}

func (c *HybridCache) startMemoryCleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		now := time.Now().UnixNano()

		keys := c.memoryCache.Keys()

		for _, k := range keys {
			if val, ok := c.memoryCache.Peek(k); ok {
				item := val.(cacheItem)
				if now > item.Expiration {
					c.memoryCache.Remove(k)
				}
			}
		}
	}
}

func CacheControlMiddleware(value string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", value)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) SetCache(w http.ResponseWriter, value string) {
	w.Header().Set("Cache-Control", value)
}

func (c *HybridCache) Get(ctx context.Context, key string) (any, bool) {
	now := time.Now().UnixNano()

	if val, found := c.memoryCache.Get(key); found {
		item := val.(cacheItem)
		if now <= item.Expiration {
			return item.Value, true
		}
	}

	if c.dataDir != "" {
		filePath := c.getSafePath(key)
		if data, err := os.ReadFile(filePath); err == nil {
			return data, true
		}
	}

	return nil, false
}

func (c *HybridCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	var data []byte

	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		data = b
	}

	c.save(key, data, ttl)
	return nil
}
