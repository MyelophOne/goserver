package goserver

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"net/http/pprof"
)

func StartPprof() {
	if IsDev() {
		go func() {
			now := time.Now().Format("2006/01/02 15:04:05")
			fmt.Println("[goserver]", now, "starting pprof at http://localhost:6060/debug/pprof/")
			if err := http.ListenAndServe("localhost:6060", nil); err != nil {
				fmt.Printf("[goserver] pprof failed: %s\n", err)
			}
		}()
	}
}

func (s *Server) EnablePprof() {
	if !s.Config.metricsEnabled {
		return
	}

	token := s.Config.metricsToken
	if token == "" {
		s.Logger.Println("WARNING: MetricsEnabled is true, but MetricsToken is empty! Pprof will NOT be mounted for security reasons.")
		return
	}

	authMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			providedToken := r.URL.Query().Get("token")

			if subtle.ConstantTimeCompare([]byte(providedToken), []byte(token)) != 1 {
				s.RenderErrorJSON(w, r, http.StatusForbidden, "Access denied: Invalid metrics token")
				return
			}

			next.ServeHTTP(w, r)
		}
	}

	s.GET("/debug/pprof/", authMiddleware(pprof.Index))

	s.GET("/debug/pprof/cmdline", authMiddleware(pprof.Cmdline))

	s.GET("/debug/pprof/profile", authMiddleware(pprof.Profile))

	s.GET("/debug/pprof/symbol", authMiddleware(pprof.Symbol))

	s.GET("/debug/pprof/trace", authMiddleware(pprof.Trace))

	s.Logger.Println("pprof profiling endpoints enabled at /debug/pprof/")
}
