package goserver

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) RedirectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		} else if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
			scheme = strings.ToLower(strings.TrimSpace(
				strings.Split(forwardedProto, ",")[0],
			))
		}

		needsRedirect := false
		newHost := host
		newPath := r.URL.Path
		newScheme := scheme

		isLocal := host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:")

		if prefix, found := strings.CutPrefix(host, "www."); found {
			newHost = prefix
			needsRedirect = true
		}

		if len(newPath) > 1 && strings.HasSuffix(newPath, "/") && !strings.HasPrefix(newPath, "/assets/") {
			newPath = strings.TrimSuffix(newPath, "/")
			needsRedirect = true
		}

		if scheme == "http" && !isLocal {
			newScheme = "https"
			needsRedirect = true
		}

		if needsRedirect {
			newURL := fmt.Sprintf("%s://%s%s", newScheme, newHost, newPath)
			if r.URL.RawQuery != "" {
				newURL += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, newURL, http.StatusMovedPermanently)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) Redirect(w http.ResponseWriter, r *http.Request, code int, location string) {
	if location == "" {
		if s.ResponseMode == "json" {
			s.RenderErrorJSON(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)+": empty redirect location")
		} else {
			s.RenderError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)+": empty redirect location")
		}
		return
	}

	if location[0] == '/' {
		if r.URL.RawQuery != "" && code == http.StatusTemporaryRedirect {
			location += "?" + r.URL.RawQuery
		}
	}

	http.Redirect(w, r, location, code)
}
