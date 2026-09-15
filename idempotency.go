package goserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/singleflight"
)

type idempotencyScopeKey struct{}
type idempotencyScope struct{ Tenant, Subject string }

func WithIdempotencyScope(ctx context.Context, tenant, subject string) context.Context {
	return context.WithValue(ctx, idempotencyScopeKey{}, idempotencyScope{tenant, subject})
}

type CachedResponse struct {
	Headers     map[string][]string `json:"headers"`
	Body        []byte              `json:"body"`
	Status      int                 `json:"status"`
	Fingerprint string              `json:"fingerprint"`
	ExpiresAt   time.Time           `json:"expires_at"`
}

const maxIdempotencyResponse = 1 << 20

var errIdempotencyResponseTooLarge = errors.New("idempotency response exceeds limit")

type responseInterceptor struct {
	header      http.Header
	body        *bytes.Buffer
	status      int
	wroteHeader bool
	err         error
}

func newResponseInterceptor() *responseInterceptor {
	return &responseInterceptor{header: make(http.Header), body: new(bytes.Buffer), status: http.StatusOK}
}
func (r *responseInterceptor) Header() http.Header { return r.header }
func (r *responseInterceptor) WriteHeader(code int) {
	if code < 200 || r.wroteHeader {
		return
	}
	r.status, r.wroteHeader = code, true
}
func (r *responseInterceptor) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	if r.err != nil {
		return 0, r.err
	}
	if len(b) > maxIdempotencyResponse-r.body.Len() {
		r.err = errIdempotencyResponseTooLarge
		return 0, r.err
	}
	return r.body.Write(b)
}

func idempotencyHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Server) IdempotencyMiddleware(next http.Handler) http.Handler {
	var group singleflight.Group
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		fail := func(code int, msg string) { s.RenderErrorJSON(w, r, code, msg) }
		scope, ok := r.Context().Value(idempotencyScopeKey{}).(idempotencyScope)
		if !ok || scope.Subject == "" {
			fail(401, "Authenticated idempotency scope required")
			return
		}
		if len(key) > 256 {
			fail(400, "Idempotency-Key too long")
			return
		}
		limit := s.Config.MaxBodySize
		if limit <= 0 {
			limit = 1 << 20
		}
		if r.Body == nil {
			r.Body = http.NoBody
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		_ = r.Body.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				fail(413, "Request body too large")
			} else {
				fail(400, "Cannot read request body")
			}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		cacheKey := "idemp:v2:" + idempotencyHash([]string{r.Host, scope.Tenant, scope.Subject, r.Method, r.URL.Path, key})
		fingerprint := idempotencyHash([]string{r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"), string(body)})
		replayed := false
		result, err, shared := group.Do(cacheKey, func() (any, error) {
			if s.Cache != nil {
				if value, found := s.Cache.Get(r.Context(), cacheKey); found {
					if data, ok := value.([]byte); ok {
						var cached CachedResponse
						if json.Unmarshal(data, &cached) == nil && time.Now().Before(cached.ExpiresAt) {
							replayed = true
							return cached, nil
						}
					}
				}
			}
			interceptor := newResponseInterceptor()
			next.ServeHTTP(interceptor, r)
			if interceptor.err != nil {
				return nil, interceptor.err
			}
			response := CachedResponse{Headers: interceptor.header.Clone(), Body: interceptor.body.Bytes(), Status: interceptor.status, Fingerprint: fingerprint, ExpiresAt: time.Now().Add(24 * time.Hour)}
			delete(response.Headers, "Set-Cookie")
			delete(response.Headers, "X-Request-Id")
			if s.Cache != nil && response.Status >= 200 && response.Status < 400 {
				data, err := json.Marshal(response)
				if err != nil {
					return nil, err
				}
				if err := s.Cache.Set(r.Context(), cacheKey, data, 24*time.Hour); err != nil {
					s.Logger.Printf("ERROR: idempotency persistence failed: %v", err)
				}
			}
			return response, nil
		})
		if err != nil {
			fail(500, "Idempotency response could not be recorded")
			return
		}
		response := result.(CachedResponse)
		if response.Fingerprint != fingerprint {
			fail(409, "Idempotency-Key reused with a different request")
			return
		}
		for k, values := range response.Headers {
			w.Header()[k] = append([]string(nil), values...)
		}
		if replayed || shared {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		w.WriteHeader(response.Status)
		_, _ = w.Write(response.Body)
	})
}
