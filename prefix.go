package goserver

import (
	"net/http"
	"strings"
)

func (s *Server) APIPrefixMiddleware(next http.Handler) http.Handler {
	prefix := s.Config.APIPrefix

	prefix = strings.TrimSpace(prefix)

	if prefix == "" || prefix == "/" {
		return next
	}

	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	prefix = strings.TrimSuffix(prefix, "/")
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
