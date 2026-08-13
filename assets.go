package goserver

import (
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) StaticAssetsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			reqCopy := new(http.Request)
			*reqCopy = *r

			reqCopy.URL.Path = strings.TrimPrefix(r.URL.Path, "/assets")

			s.PublicFiles(w, reqCopy)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) PublicFiles(w http.ResponseWriter, r *http.Request) {
	filePath := filepath.Join("assets", r.URL.Path)
	cleanPath := filepath.Clean(filePath)

	if strings.Contains(cleanPath, "..") {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(filepath.Ext(cleanPath))
	switch ext {
	case ".css", ".js", ".woff", ".woff2", ".ttf", ".eot":
		w.Header().Set("Cache-Control", "public, max-age=604800")
	case ".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".avif":
		w.Header().Set("Cache-Control", "public, max-age=2592000")
	case ".html", ".htm", ".txt", ".xml":
		w.Header().Set("Cache-Control", "public, max-age=60")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	s.fileServer.ServeHTTP(w, r)
}
