package goserver

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
)

type wildcardOrigin struct {
	scheme  string
	pattern string
}

func (wo wildcardOrigin) match(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if u.Scheme != wo.scheme {
		return false
	}

	host := u.Hostname()

	if host == wo.pattern {
		return true
	}

	if strings.HasSuffix(host, "."+wo.pattern) {
		return true
	}

	return false
}

func normalizeWildcardOrigin(o string) wildcardOrigin {
	o = strings.TrimSpace(o)

	if after, ok := strings.CutPrefix(o, "http://"); ok {
		o = after
		return wildcardOrigin{scheme: "http", pattern: strings.TrimPrefix(o, "*.")}
	}

	if after, ok := strings.CutPrefix(o, "https://"); ok {
		o = after
		return wildcardOrigin{scheme: "https", pattern: strings.TrimPrefix(o, "*.")}
	}

	o = strings.TrimPrefix(o, "*.")
	return wildcardOrigin{scheme: "https", pattern: o}
}

func (s *Server) CSRFMiddleware(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()

	env := s.Config.CsrfTrustedOrigins
	var wildcardList []wildcardOrigin
	var exactOrigins []string

	useAllOrigins := false

	if env != "" {
		if strings.TrimSpace(env) == "*" {
			useAllOrigins = true
		} else {
			parts := strings.SplitSeq(env, ",")
			for origin := range parts {
				origin = strings.TrimSpace(origin)
				if origin == "" {
					continue
				}

				if strings.Contains(origin, "*") {
					wildcardList = append(wildcardList, normalizeWildcardOrigin(origin))
				} else {
					if err := cop.AddTrustedOrigin(origin); err != nil {
						fmt.Printf("failed to add trusted origin %q: %v", origin, err)
					}

					exactOrigins = append(exactOrigins, origin)
				}
			}
		}
	}

	var cache sync.Map

	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("CSRF check failed"))
	}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if useAllOrigins {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			if r.Method != "OPTIONS" {
				next.ServeHTTP(w, r)
			}
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			cop.Handler(next).ServeHTTP(w, r)
			return
		}

		if v, ok := cache.Load(origin); ok {
			if v.(bool) {
				next.ServeHTTP(w, r)
				return
			}
			cop.Handler(next).ServeHTTP(w, r)
			return
		}

		for _, rule := range wildcardList {
			if rule.match(origin) {
				cache.Store(origin, true)
				next.ServeHTTP(w, r)
				return
			}
		}

		if slices.Contains(exactOrigins, origin) {
			cache.Store(origin, true)
			next.ServeHTTP(w, r)
			return
		}

		cache.Store(origin, false)
		cop.Handler(next).ServeHTTP(w, r)
	})
}
