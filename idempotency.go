package goserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/sync/singleflight"
)

var idempGroup singleflight.Group

type CachedResponse struct {
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
	Status  int                 `json:"status"`
}

type responseInterceptor struct {
	header http.Header
	body   *bytes.Buffer
	status int
}

func newResponseInterceptor() *responseInterceptor {
	return &responseInterceptor{
		header: make(http.Header),
		status: http.StatusOK,
		body:   new(bytes.Buffer),
	}
}

func (r *responseInterceptor) Header() http.Header {
	return r.header
}

func (r *responseInterceptor) WriteHeader(statusCode int) {
	r.status = statusCode
}

func (r *responseInterceptor) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (s *Server) IdempotencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idempKey := r.Header.Get("Idempotency-Key")

		if idempKey == "" || r.Method == http.MethodGet || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		cacheKey := "idemp:" + r.Method + ":" + r.URL.Path + ":" + idempKey

		result, err, shared := idempGroup.Do(cacheKey, func() (any, error) {
			if s.Cache != nil {
				if cachedBytes, found := s.Cache.Get(r.Context(), cacheKey); found {
					var cachedResp CachedResponse
					if err := json.Unmarshal(cachedBytes.([]byte), &cachedResp); err == nil {
						return cachedResp, nil
					}
				}
			}

			interceptor := newResponseInterceptor()
			next.ServeHTTP(interceptor, r)

			respData := CachedResponse{
				Status:  interceptor.status,
				Headers: interceptor.header,
				Body:    interceptor.body.Bytes(),
			}

			if s.Cache != nil && respData.Status >= 200 && respData.Status < 400 {
				respBytes, _ := json.Marshal(respData)
				if err := s.Cache.Set(r.Context(), cacheKey, respBytes, 24*time.Hour); err != nil {
					s.Logger.Printf("failed to set cache for key %s: %v", cacheKey, err)
				}
			}

			return respData, nil
		})

		if err != nil {
			s.RenderErrorJSON(w, r, http.StatusInternalServerError, "Internal Server Error during idempotency processing")
			return
		}

		cachedResp := result.(CachedResponse)

		for k, values := range cachedResp.Headers {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}

		isReplayed := shared || w.Header().Get("Idempotent-Replayed") != ""
		if isReplayed {
			w.Header().Set("Idempotent-Replayed", "true")
		} else {
			cachedResp.Headers["Idempotent-Replayed"] = []string{"true"}
		}

		w.WriteHeader(cachedResp.Status)
		if _, err := w.Write(cachedResp.Body); err != nil {
			s.Logger.Printf("failed to write response body: %v", err)
		}
	})
}
