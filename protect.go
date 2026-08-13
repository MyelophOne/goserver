package goserver

import (
	"net/http"
	"regexp"
	"strings"
)

func (s *Server) OutdatedBrowserMiddleware(next http.Handler) http.Handler {
	outdatedPatterns := []*regexp.Regexp{
		regexp.MustCompile(`MSIE [1-9]\.`),
		regexp.MustCompile(`Firefox/[1-4][0-9]\.`),
		regexp.MustCompile(`Chrome/[1-4][0-9]\.`),
		regexp.MustCompile(`Safari/[1-5]\.`),
		regexp.MustCompile(`Opera/[1-9]\.`),
		regexp.MustCompile(`Android [1-4]\.`),
		regexp.MustCompile(`iPhone OS [1-8]_`),
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent := r.Header.Get("User-Agent")
		if userAgent == "" {
			next.ServeHTTP(w, r)
			return
		}

		for _, pattern := range outdatedPatterns {
			if pattern.MatchString(userAgent) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("Upgrade", "modern browser")
				w.WriteHeader(http.StatusUpgradeRequired)
				if _, err := w.Write([]byte("Your browser is outdated. Please upgrade to a modern browser.")); err != nil {
					s.Logger.Printf("failed to write response: %v", err)
				}
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) MaliciousRequestMiddleware(next http.Handler) http.Handler {
	maliciousPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)python-requests`),
		regexp.MustCompile(`(?i)go-http-client`),
		regexp.MustCompile(`(?i)curl`),
		regexp.MustCompile(`(?i)wget`),
		regexp.MustCompile(`(?i)perl`),
		regexp.MustCompile(`(?i)nmap`),
		regexp.MustCompile(`(?i)sqlmap`),
		regexp.MustCompile(`(?i)wpscan`),
		regexp.MustCompile(`(?i)nikto`),
		regexp.MustCompile(`(?i)masscan`),
		regexp.MustCompile(`(?i)zgrab`),
		regexp.MustCompile(`(?i)acunetix`),
		regexp.MustCompile(`(?i)netsparker`),
		regexp.MustCompile(`(?i)burp`),
		regexp.MustCompile(`(?i)ahrefsbot`),
		regexp.MustCompile(`(?i)semrushbot`),
		regexp.MustCompile(`(?i)mj12bot`),
		regexp.MustCompile(`(?i)dotbot`),
		regexp.MustCompile(`(?i)headlesschrome`),
		regexp.MustCompile(`(?i)phantomjs`),
		regexp.MustCompile(`(?i)scrapy`),
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent := r.Header.Get("User-Agent")

		if userAgent == "" {
			s.RenderError(w, r, http.StatusForbidden, "Forbidden: A User-Agent header is required.")
			return
		}

		for _, pattern := range maliciousPatterns {
			if pattern.MatchString(userAgent) {
				s.RenderError(w, r, http.StatusForbidden, "Forbidden: Your request has been blocked.")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) LimitBodyMiddleware(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) BlockMaliciousPathsMiddleware(next http.Handler) http.Handler {
	blockedPaths := []string{
		"wp-login.php",
		"wp-admin",
		"wp-content",
		"wp-includes",
		"xmlrpc.php",
		"phpmyadmin",
		"/telescope/",
		"/swagger/",
		"/swagger-ui.html",
		"/actuator/env",
		"pma",
		"adminer",
		"admin.php",
		"login.php",
		"index.php",
		"config.php",
		"setup.php",
		"install.php",
		"installer.php",
		"eval-stdin.php",
		".env",
		".git",
		".git/config",
		".gitignore",
		".svn",
		".hg",
		".aws",
		".aws/credentials",
		".docker",
		"docker-compose.yml",
		"docker-compose.yaml",
		"dockerfile",
		".npmrc",
		".yarnrc",
		".ssh",
		"id_rsa",
		"id_dsa",
		".aws/credentials",
		"/etc/passwd",
		"/etc/shadow",
		"/proc/self/environ",
		"/proc/version",
		"/.well-known/",
		".log",
		".log",
		".bak",
		".old",
		".backup",
		".sql",
		".sqlite",
		".db",
		".tar",
		".rar",
		".7z",
		"eval-stdin.php",
		"shell.php",
		"cmd.php",
		"upload.php",
		"backdoor",
		"cgi-bin",
		".htaccess",
		".htpasswd",
		"..",
		"%2e%2e",
	}

	blockedExtensions := []string{
		".php", ".php5", ".phtml", ".phar",
		".asp", ".aspx", ".ascx", ".asmx", ".ashx",
		".jsp", ".jspx", ".do", ".action",
		".cgi", ".pl", ".py", ".fcgi",
		".rb", ".rhtml",
		".bak", ".old", ".backup", ".sql", ".sqlite", ".db",
		".tar", ".zip", ".rar", ".7z",
		".sh", ".exe", ".dll",
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := strings.ToLower(r.URL.Path)

		for _, path := range blockedPaths {
			if strings.Contains(requestPath, path) {
				s.RenderError(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
				return
			}
		}

		for _, ext := range blockedExtensions {
			if strings.HasSuffix(requestPath, ext) {
				s.RenderError(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
