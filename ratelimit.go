package goserver

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/sync/singleflight"
)

type RateLimiter struct {
	group    singleflight.Group
	cache    *lru.Cache
	limitStr string
	rate     int
	window   time.Duration
}

type ClientData struct {
	windowStart int64
	count       int64
}

func (s *Server) WithRateLimiter(size int, rate int, window time.Duration) func(http.Handler) http.Handler {
	if size <= 0 {
		size = 10000
	}

	cache, err := lru.New(size)
	if err != nil {
		panic(err)
	}

	rl := &RateLimiter{
		cache:    cache,
		rate:     rate,
		limitStr: strconv.Itoa(rate),
		window:   window,
	}

	skipLocalhost := s.Config.RateLimitSkipLocalhost

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := GetRealIP(r)

			if skipLocalhost && (clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost") {
				next.ServeHTTP(w, r)
				return
			}

			now := time.Now()

			var clientData *ClientData

			if data, exists := rl.cache.Get(clientIP); exists {
				clientData = data.(*ClientData)
			} else {
				val, _, _ := rl.group.Do(clientIP, func() (any, error) {
					if d, ok := rl.cache.Get(clientIP); ok {
						return d, nil
					}

					newData := &ClientData{
						count:       0,
						windowStart: now.UnixNano(),
					}
					rl.cache.Add(clientIP, newData)
					return newData, nil
				})
				clientData = val.(*ClientData)
			}

			windowStart := atomic.LoadInt64(&clientData.windowStart)
			windowStartTime := time.Unix(0, windowStart)

			if now.Sub(windowStartTime) >= rl.window {
				if atomic.CompareAndSwapInt64(&clientData.windowStart, windowStart, now.UnixNano()) {
					atomic.StoreInt64(&clientData.count, 0)
				}
				windowStart = atomic.LoadInt64(&clientData.windowStart)
				windowStartTime = time.Unix(0, windowStart)
			}

			currentCount := atomic.AddInt64(&clientData.count, 1)

			remaining := rl.rate - int(currentCount)
			if remaining < 0 {
				remaining = 0
			}

			resetTime := windowStartTime.Add(rl.window).Unix()

			w.Header().Set("X-RateLimit-Limit", rl.limitStr)
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime, 10))

			if currentCount > int64(rl.rate) {
				waitSec := int(windowStartTime.Add(rl.window).Sub(now).Seconds())
				if waitSec < 1 {
					waitSec = 1
				}

				w.Header().Set("Retry-After", strconv.Itoa(waitSec))
				s.RenderError(w, r, http.StatusTooManyRequests, http.StatusText(http.StatusTooManyRequests))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) RateLimitMiddleware(next http.Handler) http.Handler {
	return s.WithRateLimiter(
		s.Config.RateLimiteSize,
		s.Config.RateLimiteRate,
		s.Config.RateLimiteWindow,
	)(next)
}
