package goserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

type Tenant struct {
	Config map[string]any
	ID     string
	Name   string
}

type TenantStore struct {
	tenants map[string]*Tenant
	mu      sync.RWMutex
}

type tenantContextKey struct{}

type TenantMiddlewareConfig struct {
	HeaderKey string
	QueryKey  string
	UseDomain bool
}

func NewTenantStore() *TenantStore {
	return &TenantStore{
		tenants: make(map[string]*Tenant),
	}
}

func (ts *TenantStore) AddTenant(t *Tenant) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tenants[t.ID] = t
}

func (ts *TenantStore) RemoveTenant(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.tenants, id)
}

func (ts *TenantStore) GetTenant(id string) (*Tenant, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	t, ok := ts.tenants[id]
	return t, ok
}

func (ts *TenantStore) Middleware(cfg *TenantMiddlewareConfig) Middleware {
	if cfg == nil {
		cfg = &TenantMiddlewareConfig{
			UseDomain: true,
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tenantID string
			if cfg.HeaderKey != "" {
				tenantID = r.Header.Get(cfg.HeaderKey)
			}
			if tenantID == "" && cfg.QueryKey != "" {
				tenantID = r.URL.Query().Get(cfg.QueryKey)
			}
			if tenantID == "" && cfg.UseDomain {
				host := r.Host
				hostname := host
				if h, port, err := net.SplitHostPort(host); err == nil {
					if port == "80" || port == "443" {
						hostname = h
					} else {
						hostname = host
					}
				}
				ts.mu.RLock()
				for id, t := range ts.tenants {
					domain, ok := t.Config["domain"].(string)
					if !ok {
						continue
					}
					if domain == hostname || domain == host || matchWildcardExtended(domain, hostname) {
						tenantID = id
						break
					}
				}
				ts.mu.RUnlock()
			}
			if tenantID == "" {
				http.Error(w, "Tenant not found", http.StatusForbidden)
				return
			}
			tenant, ok := ts.GetTenant(tenantID)
			if !ok {
				http.Error(w, fmt.Sprintf("Tenant %s not registered", tenantID), http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), tenantContextKey{}, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func matchWildcardExtended(pattern, hostname string) bool {
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}
	patternParts := strings.Split(pattern, ".")[1:]
	hostParts := strings.Split(hostname, ".")
	if len(hostParts) < len(patternParts) {
		return false
	}
	for i := 1; i <= len(patternParts); i++ {
		if patternParts[len(patternParts)-i] != hostParts[len(hostParts)-i] {
			return false
		}
	}
	return true
}

func (ts *TenantStore) MiddlewareByDomain() Middleware {
	return ts.Middleware(nil)
}

func GetTenant(r *http.Request) *Tenant {
	t, ok := r.Context().Value(tenantContextKey{}).(*Tenant)
	if !ok {
		return nil
	}
	return t
}
