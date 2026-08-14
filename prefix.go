package goserver

import (
	"net/http"
	"strings"
)

func (s *Server) APIPrefixMiddleware(next http.Handler) http.Handler {
	prefix := normalizeAPIPrefix(s.Config.APIPrefix)
	if prefix == "" {
		return next
	}
	prefixWithSlash := prefix + "/"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix {
			r.URL.Path = "/"
			if r.URL.RawPath != "" {
				r.URL.RawPath = "/"
			}
		} else if strings.HasPrefix(r.URL.Path, prefixWithSlash) {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)

			if r.URL.RawPath != "" {
				r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, prefix)
			}
		}

		next.ServeHTTP(w, r)
	})
}

func normalizeAPIPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimSuffix(prefix, "/")
}
