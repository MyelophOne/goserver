package goserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type retryTestTransport func(*http.Request) (*http.Response, error)

func (fn retryTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

type retryTestBody struct {
	*strings.Reader
	closed bool
}

func (b *retryTestBody) Close() error { b.closed = true; return nil }
func (b *retryTestBody) Read(p []byte) (int, error) {
	if b.closed {
		return 0, errors.New("read from closed body")
	}
	return b.Reader.Read(p)
}

func retryClient(t *testing.T, rt retryTestTransport, opts ...ClientOption) (*HttpClient, string) {
	t.Helper()
	host := strings.ReplaceAll(t.Name(), "/", "-") + ".test"
	t.Cleanup(func() { cbMap.Delete(host) })
	base := []ClientOption{
		WithRandomDelayMin(0), WithRandomDelayMax(0), WithCBResetTimeout(time.Hour),
		func(c *ClientConfig) { c.BaseBackoffDelay = 0; c.MaxBackoffDelay = 0 },
	}
	c := NewHttpClient(append(base, opts...)...)
	c.client.Transport = rt
	return c, "https://" + host
}

func TestClientRetryPreservesFinalResponse(t *testing.T) {
	for _, status := range []int{429, 503, 401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var bodies []*retryTestBody
			c, target := retryClient(t, func(req *http.Request) (*http.Response, error) {
				b := &retryTestBody{Reader: strings.NewReader("upstream reason")}
				bodies = append(bodies, b)
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: b, Request: req}, nil
			}, WithMaxRetries(2), WithCBMaxFailures(100), WithProxyFunc(func() *url.URL { return nil }))
			resp, err := c.Get(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != "upstream reason" || resp.StatusCode != status {
				t.Fatalf("lost response: %q, %v", body, err)
			}
			if len(bodies) != 3 {
				t.Fatalf("attempts = %d, want 3", len(bodies))
			}
			if !bodies[0].closed || !bodies[1].closed || bodies[2].closed {
				t.Fatal("incorrect response body ownership")
			}
		})
	}
}

func TestClientBreakerPreservesLastResponse(t *testing.T) {
	attempts := 0
	c, target := retryClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 429, Header: make(http.Header), Body: &retryTestBody{Reader: strings.NewReader("rate limited")}}, nil
	}, WithMaxRetries(5))
	resp, err := c.Get(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != "rate limited" || attempts != 5 {
		t.Fatalf("lost final result: attempts=%d body=%q err=%v", attempts, body, err)
	}
	_, err = c.Get(context.Background(), target)
	var open *CircuitBreakerOpenError
	if !errors.Is(err, ErrCircuitBreakerOpen) || !errors.As(err, &open) || !strings.Contains(open.Cause.Error(), "HTTP 429") {
		t.Fatalf("missing breaker reason: %v", err)
	}
	if attempts != 5 {
		t.Fatal("open breaker sent another request")
	}
}

func TestClientDisableCircuitBreakerBypassesSharedState(t *testing.T) {
	attempts := 0
	rt := retryTestTransport(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: &retryTestBody{Reader: strings.NewReader("unavailable")}}, nil
	})
	enabled, target := retryClient(t, rt, WithMaxRetries(0), WithCBMaxFailures(1))
	resp, err := enabled.Get(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	disabled, _ := retryClient(t, rt, WithDisableCircuitBreaker(true), WithMaxRetries(5))
	resp, err = disabled.Get(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != "unavailable" || attempts != 7 {
		t.Fatalf("disabled breaker still interfered: attempts=%d body=%q err=%v", attempts, body, err)
	}
	_, err = enabled.Get(context.Background(), target)
	if !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Fatalf("disabled client changed shared breaker state: %v", err)
	}
}

func TestClientBreakerPreservesTransportError(t *testing.T) {
	cause := errors.New("TLS handshake failed")
	c, target := retryClient(t, func(req *http.Request) (*http.Response, error) { return nil, cause }, WithMaxRetries(5), WithCBMaxFailures(1))
	_, err := c.Get(context.Background(), target)
	if !errors.Is(err, cause) || errors.Is(err, ErrCircuitBreakerOpen) {
		t.Fatalf("original cause replaced: %v", err)
	}
	_, err = c.Get(context.Background(), target)
	if !errors.Is(err, cause) || !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Fatalf("historical cause missing: %v", err)
	}
}

func TestClientUnauthorizedWithoutProxyDoesNotOpenBreaker(t *testing.T) {
	attempts := 0
	c, target := retryClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("require_login"))}, nil
	}, WithMaxRetries(5), WithCBMaxFailures(1))
	for i := 0; i < 2; i++ {
		resp, err := c.Get(context.Background(), target)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if attempts != 2 {
		t.Fatalf("401 was retried: %d", attempts)
	}
}

func TestClientRetryReplaysRequestBody(t *testing.T) {
	attempts := 0
	c, target := retryClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil || string(body) != "payload" {
			t.Errorf("attempt %d: body=%q err=%v", attempts, body, err)
		}
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("failed"))}, nil
	}, WithDisableCircuitBreaker(true), WithMaxRetries(2))
	resp, err := c.Post(context.Background(), target, []byte("payload"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if attempts != 3 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestClientDoesNotRetryUnreplayableBody(t *testing.T) {
	attempts := 0
	c, target := retryClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		_, _ = io.Copy(io.Discard, req.Body)
		req.Body.Close()
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("failed"))}, nil
	}, WithMaxRetries(5))
	req, _ := http.NewRequest(http.MethodPost, target, io.NopCloser(strings.NewReader("stream")))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if attempts != 1 {
		t.Fatalf("retried consumed body: %d", attempts)
	}
}

func TestClientCancellationDoesNotTripBreaker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, target := retryClient(t, func(req *http.Request) (*http.Response, error) {
		cancel()
		return nil, req.Context().Err()
	}, WithCBMaxFailures(1))
	_, err := c.Get(ctx, target)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	u, _ := url.Parse(target)
	cb := getCircuitBreaker(u.Host, c.config)
	if !cb.allowRequest() {
		t.Fatal("caller cancellation opened the breaker")
	}
}
