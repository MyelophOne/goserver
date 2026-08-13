package goserver

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	botKey contextKey = "isBot"
	aiKey  contextKey = "isAi"
)

var knownBots = []string{
	"googlebot",
	"bingbot",
	"slurp",
	"duckduckbot",
	"baiduspider",
	"yandexbot",
	"facebookexternalhit",
	"twitterbot",
	"linkedinbot",
}

var knownAi = []string{
	"chatgpt",
	"gptbot",
	"claude",
	"perplexity",
	"anthropic-ai",
	"python-requests",
	"curl",
}

func (s *Server) BotAndAiDetectionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := strings.ToLower(r.UserAgent())

		isBot := false
		for _, b := range knownBots {
			if strings.Contains(ua, b) {
				isBot = true
				break
			}
		}

		isAi := false
		for _, a := range knownAi {
			if strings.Contains(ua, a) {
				isAi = true
				break
			}
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, botKey, isBot)
		ctx = context.WithValue(ctx, aiKey, isAi)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func IsBot(r *http.Request) bool {
	if v, ok := r.Context().Value(botKey).(bool); ok {
		return v
	}
	return false
}

func IsAi(r *http.Request) bool {
	if v, ok := r.Context().Value(aiKey).(bool); ok {
		return v
	}
	return false
}

func GetClientType(r *http.Request) string {
	if IsAi(r) {
		return "ai"
	}
	if IsBot(r) {
		return "bot"
	}
	return "human"
}
