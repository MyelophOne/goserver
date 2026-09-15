package goserver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHybridCacheSWRStartsOneBackgroundRefreshPerKey(t *testing.T) {
	cache := NewCache(16, "")
	cache.memoryCache.Add("stale", cacheItem{Value: []byte("old"), Expiration: time.Now().Add(-time.Second).UnixNano()})

	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	generate := func(context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return []byte("new"), nil
	}

	var group sync.WaitGroup
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			value, err := cache.GetOrSetSWR(context.Background(), "stale", time.Minute, generate)
			if err != nil || string(value) != "old" {
				t.Errorf("stale value=%q err=%v", value, err)
			}
		}()
	}
	group.Wait()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("background generator calls=%d, want 1", got)
	}
	close(release)
}
