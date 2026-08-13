package goserver

import "net/http"

func (s *Server) FaviconMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" || r.URL.Path == "/favicon.svg" || r.URL.Path == "/favicon.png" {
			w.Header().Set("Cache-Control", "public, max-age=604800")
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
