package goserver

import (
	"net/http"
)

func (s *Server) defaultNotFound(w http.ResponseWriter, r *http.Request) {
	if s.ResponseMode == "json" {
		s.RenderErrorJSON(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
	} else {
		s.RenderError(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
	}
}

func (s *Server) defaultInternalError(w http.ResponseWriter, r *http.Request) {
	if s.ResponseMode == "json" {
		s.RenderErrorJSON(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	} else {
		s.RenderError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}
}

func (s *Server) Defaults() {
	s.Use(s.RequestIDMiddleware)
	if IsDev() {
		s.Use(s.LogRequest)
	} else {
		s.Use(s.ProdAccessLogger)
	}
	s.Use(s.MetricsMiddleware)
	s.Use(s.LoadSheddingMiddleware)
	s.Use(s.RecoveryMiddleware(s.errorHandler))
	s.Use(s.WithRateLimiter(s.Config.RateLimiteSize, s.Config.RateLimiteRate, s.Config.RateLimiteWindow))
	s.Use(s.FaviconMiddleware)
	s.Use(s.StaticAssetsMiddleware)
	s.Use(s.APIPrefixMiddleware)
	s.Use(s.RedirectMiddleware)
	s.Use(s.OutdatedBrowserMiddleware)
	s.Use(s.MaliciousRequestMiddleware)
	s.Use(s.BlockMaliciousPathsMiddleware)
	s.Use(s.SanitizeURLMiddleware)
	s.Use(s.CSRFMiddleware)
	s.Use(s.SecurityMiddleware(DefaultSecurityOptions))
	s.Use(s.TimeoutMiddleware(s.Config.ReadTimeout))
	s.Use(s.LimitBodyMiddleware(s.Config.MaxBodySize))
	s.Use(s.BotAndAiDetectionMiddleware)
	s.Use(s.IdempotencyMiddleware)
	s.GET("/robots.txt", s.PublicFiles)
	s.EnablePprof()
}
