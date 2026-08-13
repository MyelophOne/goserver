package goserver

import (
	"context"
	"net/http"
)

const goserverKey ctxKey = "goserver"

func (s *Server) ServerContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if existing := r.Context().Value("goserver"); existing == nil {
			ctx := context.WithValue(r.Context(), goserverKey, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			next.ServeHTTP(w, r)
		}
	})
}

func GetServer(ctx context.Context) *Server {
	if s, ok := ctx.Value(goserverKey).(*Server); ok {
		return s
	}
	return nil
}

func MustGetServer(w http.ResponseWriter, r *http.Request) *Server {
	s := GetServer(r.Context())
	if s == nil {
		http.Error(w, "server not found in context", http.StatusInternalServerError)
		return nil
	}
	return s
}
