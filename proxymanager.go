package goserver

import (
	"context"
	"math/rand"
	"net/url"
	"sync"
	"time"
)

type ProxyProvider interface {
	FetchProxies(ctx context.Context) ([]*url.URL, error)
}

type ProxyManager struct {
	badProxies  map[string]time.Time
	proxies     []*url.URL
	providers   []ProxyProvider
	badDuration time.Duration
	mu          sync.RWMutex
}

func NewProxyManager(badDuration time.Duration, providers ...ProxyProvider) *ProxyManager {
	return &ProxyManager{
		proxies:     make([]*url.URL, 0),
		badProxies:  make(map[string]time.Time),
		badDuration: badDuration,
		providers:   providers,
	}
}

func (m *ProxyManager) UpdateSynchronously(ctx context.Context) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allProxies []*url.URL
	var fetchErrors []error

	for _, p := range m.providers {
		wg.Add(1)
		go func(provider ProxyProvider) {
			defer wg.Done()

			list, err := provider.FetchProxies(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				fetchErrors = append(fetchErrors, err)
			} else {
				allProxies = append(allProxies, list...)
			}
		}(p)
	}

	wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.proxies = allProxies

	now := time.Now()
	for k, t := range m.badProxies {
		if now.Sub(t) > m.badDuration {
			delete(m.badProxies, k)
		}
	}

	if len(fetchErrors) > 0 {
		return fetchErrors[0]
	}
	return nil
}

func (m *ProxyManager) StartAutoUpdate(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = m.UpdateSynchronously(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *ProxyManager) MarkBad(u *url.URL) {
	if u == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.badProxies[u.String()] = time.Now()
}

func (m *ProxyManager) GetRandomProxy() *url.URL {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.proxies)
	if total == 0 {
		return nil
	}

	now := time.Now()

	startIdx := rand.Intn(total)

	for i := 0; i < total; i++ {
		idx := (startIdx + i) % total
		p := m.proxies[idx]

		if bannedTime, isBad := m.badProxies[p.String()]; !isBad || now.Sub(bannedTime) > m.badDuration {
			return p
		}
	}

	return nil
}
