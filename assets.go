package goserver

import (
	"bytes"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
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

		if HasPublicFile(r.URL.Path) {
			s.PublicFiles(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) PublicFiles(w http.ResponseWriter, r *http.Request) {
	cleanPath, ok := publicAssetPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(filepath.Ext(cleanPath))
	switch ext {
	case ".css", ".js", ".woff", ".woff2", ".ttf", ".eot", ".ico":
		w.Header().Set("Cache-Control", "public, max-age=604800")
	case ".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".avif":
		w.Header().Set("Cache-Control", "public, max-age=2592000")
	case ".html", ".htm", ".txt", ".xml":
		w.Header().Set("Cache-Control", "public, max-age=60")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	if data, ok := embeddedPublicAsset(cleanPath); ok {
		http.ServeContent(w, r, filepath.Base(cleanPath), time.Time{}, bytes.NewReader(data))
		return
	}
	if generatedWorker, ok := generatedSiteSearchWorker(cleanPath); ok {
		if data, err := os.ReadFile(generatedWorker); err == nil {
			http.ServeContent(w, r, filepath.Base(cleanPath), time.Time{}, bytes.NewReader(data))
			return
		}
	}
	if info, err := os.Stat(filepath.Join("assets", filepath.FromSlash(cleanPath))); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	s.fileServer.ServeHTTP(w, r)
}

func HasPublicFile(urlPath string) bool {
	cleanPath, ok := publicAssetPath(urlPath)
	if !ok {
		return false
	}
	if _, ok := embeddedPublicAsset(cleanPath); ok {
		return true
	}
	if generatedWorker, ok := generatedSiteSearchWorker(cleanPath); ok {
		info, err := os.Stat(generatedWorker)
		return err == nil && !info.IsDir()
	}
	info, err := os.Stat(filepath.Join("assets", filepath.FromSlash(cleanPath)))
	return err == nil && !info.IsDir()
}

func generatedSiteSearchWorker(cleanPath string) (string, bool) {
	switch cleanPath {
	case "site-search-worker.js", "site-search-server-worker.js":
		return filepath.Join("tmp", "site-search", cleanPath), true
	default:
		return "", false
	}
}

func publicAssetPath(urlPath string) (string, bool) {
	cleanPath := path.Clean("/" + urlPath)
	relative := strings.TrimPrefix(cleanPath, "/")
	if relative == "" || strings.Contains(relative, "\\") || strings.HasPrefix(relative, "../") {
		return "", false
	}
	return relative, true
}

func embeddedPublicAsset(name string) ([]byte, bool) {
	if _, production := sourceFS(); !production {
		return nil, false
	}
	data, err := sourceReadFile(filepath.ToSlash(filepath.Join("assets", name)))
	return data, err == nil
}
