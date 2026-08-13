package goredis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/myelophone/goserver"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

type RedisCache struct {
	client *redis.Client
	group  singleflight.Group
}

type CacheConfig struct {
	Addr         string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
}

func NewCache(cfg CacheConfig) goserver.CacheStore {
	if cfg.PoolSize == 0 {
		cfg.PoolSize = 100
	}
	if cfg.MinIdleConns == 0 {
		cfg.MinIdleConns = 10
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})

	return &RedisCache{
		client: client,
	}
}

func (r *RedisCache) GetOrSetSWR(ctx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err == nil {
		return val, nil
	}

	v, err, _ := r.group.Do(key, func() (any, error) {
		if val, err := r.client.Get(ctx, key).Bytes(); err == nil {
			return val, nil
		}

		newData, genErr := generate(ctx)
		if genErr != nil {
			return nil, genErr
		}

		if len(newData) > 0 {
			r.client.Set(ctx, key, newData, maxAge)
		}
		return newData, nil
	})

	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func (r *RedisCache) GetOrSet(ctx context.Context, key string, maxAge time.Duration, generate func(ctx context.Context) ([]byte, error)) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err == nil {
		return val, nil
	}

	v, err, _ := r.group.Do(key, func() (any, error) {
		if val, err := r.client.Get(ctx, key).Bytes(); err == nil {
			return val, nil
		}

		newData, genErr := generate(ctx)
		if genErr != nil {
			return nil, genErr
		}

		if len(newData) > 0 {
			r.client.Set(ctx, key, newData, maxAge)
		}
		return newData, nil
	})

	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func (r *RedisCache) Delete(ctx context.Context, key string) {
	r.client.Del(ctx, key)
}

func (r *RedisCache) Get(ctx context.Context, key string) (any, bool) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false
	} else if err != nil {
		return nil, false
	}
	return val, true
}

func (r *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
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

	return r.client.Set(ctx, key, data, ttl).Err()
}
