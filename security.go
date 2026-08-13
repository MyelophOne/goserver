package goserver

import (
	"fmt"
	"net/http"
)

type SecurityOptions struct {
	FrameOptions       string
	CSP                string
	ReferrerPolicy     string
	HSTSMaxAge         int64
	HSTSIncludeSub     bool
	HSTSPreload        bool
	XSSProtection      bool
	ContentTypeNoSniff bool
}

var DefaultSecurityOptions = SecurityOptions{
	HSTSMaxAge:         31536000,
	HSTSIncludeSub:     true,
	HSTSPreload:        false,
	XSSProtection:      true,
	ContentTypeNoSniff: true,
	FrameOptions:       "DENY",
	ReferrerPolicy:     "strict-origin-when-cross-origin",
}

func (s *Server) SecurityMiddleware(options SecurityOptions) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if options.HSTSMaxAge > 0 {
				hsts := fmt.Sprintf("max-age=%d", options.HSTSMaxAge)
				if options.HSTSIncludeSub {
					hsts += "; includeSubDomains"
				}
				if options.HSTSPreload {
					hsts += "; preload"
				}
				w.Header().Set("Strict-Transport-Security", hsts)
			}

			if options.XSSProtection {
				w.Header().Set("X-XSS-Protection", "1; mode=block")
			}

			if options.ContentTypeNoSniff {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			if options.FrameOptions != "" {
				w.Header().Set("X-Frame-Options", options.FrameOptions)
			}

			if options.CSP != "" {
				w.Header().Set("Content-Security-Policy", options.CSP)
			}

			if options.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", options.ReferrerPolicy)
			}

			next.ServeHTTP(w, r)
		})
	}
}
