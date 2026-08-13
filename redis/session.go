package goredis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/myelophone/goserver"
)

const redisSessionPrefix = "session:"

type SessionConfig struct {
	Addr       string
	Password   string
	DB         int
	Expiration time.Duration
}

type RedisStorage struct {
	group      singleflight.Group
	client     *redis.Client
	expiration time.Duration
}

func NewSession(sessionConfig SessionConfig) goserver.SessionStorage {
	rdb := redis.NewClient(&redis.Options{
		Addr:         sessionConfig.Addr,
		Password:     sessionConfig.Password,
		DB:           sessionConfig.DB,
		PoolSize:     100,
		MinIdleConns: 10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(fmt.Sprintf("CRITICAL: Failed to connect to Redis at %s: %v", sessionConfig.Addr, err))
	}

	exp := sessionConfig.Expiration
	if exp == 0 {
		exp = 12 * time.Hour
	}

	return &RedisStorage{
		client:     rdb,
		expiration: exp,
	}
}

func (r *RedisStorage) Get(id string) (goserver.Session, bool) {
	val, err := r.client.Get(context.Background(), redisSessionPrefix+id).Bytes()
	if err != nil {
		return nil, false
	}

	var data goserver.Session
	if err := json.Unmarshal(val, &data); err != nil {
		return nil, false
	}

	return data, true
}

func (r *RedisStorage) Set(id string, data goserver.Session) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), redisSessionPrefix+id, bytes, r.expiration).Err()
}

func (r *RedisStorage) Delete(id string) error {
	return r.client.Del(context.Background(), redisSessionPrefix+id).Err()
}

func (r *RedisStorage) GetOrSetSession(id string, generate func() goserver.Session) (goserver.Session, error) {
	if val, ok := r.Get(id); ok {
		return val, nil
	}

	val, err, _ := r.group.Do(id, func() (any, error) {
		newData := generate()

		if err := r.Set(id, newData); err != nil {
			return nil, err
		}

		return newData, nil
	})

	if err != nil {
		return nil, err
	}

	return val.(goserver.Session), nil
}

func (r *RedisStorage) GetOrSet(id string, key string, generate func() (any, error)) (any, error) {
	sfKey := fmt.Sprintf("sess_val:%s:%s", id, key)

	val, err, _ := r.group.Do(sfKey, func() (any, error) {
		currentSession, ok := r.Get(id)
		if ok {
			if val, exists := currentSession[key]; exists {
				return val, nil
			}
		} else {
			currentSession = make(goserver.Session)
		}

		newValue, err := generate()
		if err != nil {
			return nil, err
		}

		currentSession[key] = newValue

		if err := r.Set(id, currentSession); err != nil {
			return nil, err
		}

		return newValue, nil
	})

	return val, err
}
