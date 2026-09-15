package goserver

import (
	"net/http"
	"net/url"
)

func (s *Server) SanitizeURLMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalURL := r.URL.String()

		sanitizedPath := SanitizeXSS(r.URL.Path)
		if sanitizedPath == "" {
			sanitizedPath = "/"
		}

		sanitizedQuery := SanitizeXSSQuery(r.URL.Query())

		sanitizedURL := &url.URL{
			Scheme:   r.URL.Scheme,
			Host:     r.URL.Host,
			Path:     sanitizedPath,
			RawQuery: sanitizedQuery.Encode(),
		}
		cleanURL := sanitizedURL.String()

		if originalURL != cleanURL {
			if r.URL.Path == "/_gosh/site-search/query" {
				r.URL.Path = sanitizedPath
				r.URL.RawQuery = sanitizedQuery.Encode()
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet {
				http.Redirect(w, r, cleanURL, http.StatusFound)
				return
			} else {
				r.URL.Path = sanitizedPath
				r.URL.RawQuery = sanitizedQuery.Encode()
			}
		}

		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			if err := r.ParseForm(); err == nil {
				for key, vals := range r.Form {
					for i, v := range vals {
						r.Form[key][i] = SanitizeXSS(v)
					}
				}

				if r.PostForm != nil {
					for key, vals := range r.PostForm {
						for i, v := range vals {
							r.PostForm[key][i] = SanitizeXSS(v)
						}
					}
				}
			}
		}

		r.URL.Path = sanitizedPath
		r.URL.RawQuery = sanitizedQuery.Encode()

		next.ServeHTTP(w, r)
	})
}
