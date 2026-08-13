package goserver

import (
	"net/http"
	"regexp"
)

type RewriteRule struct {
	Pattern     *regexp.Regexp
	Replacement string
}

func (s *Server) RewriteMiddleware(rules map[string]string) Middleware {
	compiledRules := make([]RewriteRule, 0, len(rules))

	for from, to := range rules {
		if len(from) > 0 && from[0] == '^' {
			re := regexp.MustCompile(from)
			compiledRules = append(compiledRules, RewriteRule{Pattern: re, Replacement: to})
		} else {
			re := regexp.MustCompile("^" + regexp.QuoteMeta(from) + "$")
			compiledRules = append(compiledRules, RewriteRule{Pattern: re, Replacement: to})
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, rule := range compiledRules {
				if rule.Pattern.MatchString(r.URL.Path) {
					r.URL.Path = rule.Pattern.ReplaceAllString(r.URL.Path, rule.Replacement)
					break
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
