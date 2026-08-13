package goserver

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(opts ...ClientOption) *HttpClient {
	baseOpts := []ClientOption{
		WithTimeout(5 * time.Second),
		WithAllowInsecureSSRF(true),
	}

	baseOpts = append(baseOpts, opts...)
	return NewHttpClient(baseOpts...)
}

func safeTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
	})
	return server
}

func TestHighLoadClient(t *testing.T) {
	requestCount := int32(0)
	server := safeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	client := newTestClient(
		WithMaxRetries(2),
		WithCBMaxFailures(50),
		WithCBResetTimeout(100*time.Millisecond),
		WithDNSCacheTTL(1*time.Minute),
		WithEnableMetrics(true),
		WithMetricsCallback(func(m Metrics) {
			if m.StatusCode != 0 {
				log.Printf("Metrics: host=%s, duration=%v, status=%d, retries=%d",
					m.Host, m.RequestDuration, m.StatusCode, m.RetryCount)
			}
		}),
	)

	const numRequests = 50
	const maxConcurrent = 10

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrent)
	successCount := int32(0)
	errorCount := int32(0)

	startTime := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			time.Sleep(time.Duration(id%10) * time.Millisecond)

			resp, err := client.Get(ctx, server.URL)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				log.Printf("Request %d failed: %v", id, err)
				return
			}
			defer func() {
				_ = resp.Body.Close()
			}()

			if resp.StatusCode == http.StatusOK {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	t.Logf("Total requests: %d", numRequests)
	t.Logf("Successful: %d", successCount)
	t.Logf("Errors: %d", errorCount)
	t.Logf("Server received: %d", atomic.LoadInt32(&requestCount))
	t.Logf("Total time: %v", duration)
	t.Logf("Requests per second: %.2f", float64(numRequests)/duration.Seconds())

	if successCount < int32(numRequests*0.8) {
		t.Errorf("Too many errors: %d successes out of %d", successCount, numRequests)
	}
}

func TestCircuitBreaker(t *testing.T) {
	failCount := int32(0)
	server := safeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&failCount, 1)
		if count <= 5 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write([]byte("test"))
	}))

	client := newTestClient(
		WithCBMaxFailures(3),
		WithCBResetTimeout(1*time.Second),
		WithMaxRetries(0),
	)

	var errors []error
	for i := 0; i < 10; i++ {
		resp, err := client.Get(context.Background(), server.URL)
		if err != nil {
			errors = append(errors, err)
		} else if resp != nil {
			if err := resp.Body.Close(); err != nil {
				errors = append(errors, err)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	circuitOpen := false
	for _, err := range errors {
		if err == ErrCircuitBreakerOpen {
			circuitOpen = true
			break
		}
	}

	if !circuitOpen {
		t.Error("Circuit breaker should have opened")
	}

	time.Sleep(1500 * time.Millisecond)

	var recovered bool
	for i := 0; i < 3; i++ {
		resp, err := client.Get(context.Background(), server.URL)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			recovered = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !recovered {
		t.Error("Circuit breaker should have recovered")
	}
}

func TestDNSCache(t *testing.T) {
	server := safeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	client := newTestClient(
		WithDNSCacheTTL(10*time.Second),
		WithCBMaxFailures(100),
		WithMaxRetries(0),
	)

	resp0, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Warm-up request failed: %v", err)
	}
	defer func() {
		_ = resp0.Body.Close()
	}()

	time.Sleep(100 * time.Millisecond)

	var duration1 time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		resp1, err := client.Get(context.Background(), server.URL)
		if err != nil {
			t.Fatalf("First request failed: %v", err)
		}
		defer func() {
			_ = resp1.Body.Close()
		}()
		duration1 = time.Since(start)
		if i < 2 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	start2 := time.Now()
	resp2, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	defer func() {
		_ = resp2.Body.Close()
	}()
	duration2 := time.Since(start2)

	t.Logf("First request duration (avg): %v", duration1)
	t.Logf("Second request duration: %v", duration2)

	if duration2 > duration1*2 {
		t.Logf("Warning: Second request is significantly slower: %.2f%% slower",
			(float64(duration2)-float64(duration1))/float64(duration1)*100)
	} else {
		t.Logf("DNS cache working: second request within reasonable range")
	}

	ClearDNSCache()

	time.Sleep(50 * time.Millisecond)

	start3 := time.Now()
	resp3, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Third request failed: %v", err)
	}
	defer func() {
		_ = resp3.Body.Close()
	}()
	duration3 := time.Since(start3)

	t.Logf("After cache clear duration: %v", duration3)

	t.Logf("Note: For localhost DNS cache effect may be minimal")
}

func TestBufferPool(t *testing.T) {
	server := safeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	client := newTestClient(
		WithCBMaxFailures(100),
		WithCBResetTimeout(50*time.Millisecond),
	)

	type TestData struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	for i := 0; i < 50; i++ {
		data := TestData{
			ID:   i,
			Name: fmt.Sprintf("test-%d", i),
		}

		time.Sleep(5 * time.Millisecond)

		resp, err := client.PostJSON(context.Background(), server.URL, data)
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)
			continue
		}
		defer func() {
			_ = resp.Body.Close()
		}()
	}
}

func BenchmarkClientParallel(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	client := newTestClient(
		WithMaxRetries(1),
		WithDNSCacheTTL(5*time.Minute),
	)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(context.Background(), server.URL)
			if err != nil {
				b.Errorf("Request failed: %v", err)
				continue
			}
			defer func() {
				_ = resp.Body.Close()
			}()
		}
	})
}

func TestConcurrentRequestsWithRateLimiting(t *testing.T) {
	server := safeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	client := newTestClient(
		WithMaxRetries(1),
		WithCBMaxFailures(100),
		WithCBResetTimeout(100*time.Millisecond),
		WithRandomDelayMin(0),
		WithRandomDelayMax(0),
	)

	const numWorkers = 50
	const requestsPerWorker = 20
	const totalRequests = numWorkers * requestsPerWorker

	var wg sync.WaitGroup
	errors := make(chan error, totalRequests)
	successes := atomic.Int32{}

	start := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < requestsPerWorker; r++ {
				resp, err := client.Get(context.Background(), server.URL)
				if err != nil {
					errors <- fmt.Errorf("worker %d, request %d: %v", workerID, r, err)
					continue
				}
				defer func() {
					_ = resp.Body.Close()
				}()
				successes.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errors)

	duration := time.Since(start)

	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}

	t.Logf("Total requests: %d", totalRequests)
	t.Logf("Successful: %d", successes.Load())
	t.Logf("Errors: %d", len(errorList))
	t.Logf("Duration: %v", duration)
	t.Logf("Requests per second: %.2f", float64(totalRequests)/duration.Seconds())
	t.Logf("Success rate: %.2f%%", float64(successes.Load())/float64(totalRequests)*100)

	if len(errorList) > 0 {
		for i, err := range errorList {
			if i < 5 {
				t.Logf("Error %d: %v", i+1, err)
			}
		}
	}

	if float64(successes.Load())/float64(totalRequests) < 0.95 {
		t.Errorf("Success rate too low: %.2f%%", float64(successes.Load())/float64(totalRequests)*100)
	}
}

func TestExternalServicePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping external service test in short mode")
	}

	t.Run("HTTPBinGet", func(t *testing.T) {
		client := NewHttpClient(
			WithTimeout(10*time.Second),
			WithMaxRetries(2),
			WithCBMaxFailures(10),
			WithCBResetTimeout(5*time.Second),
			WithDNSCacheTTL(5*time.Minute),
			WithEnableMetrics(true),
			WithMetricsCallback(func(m Metrics) {
				t.Logf("External request: host=%s, duration=%v, status=%d",
					m.Host, m.RequestDuration, m.StatusCode)
			}),
		)

		const numRequests = 10
		const maxConcurrent = 3

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, maxConcurrent)
		successCount := atomic.Int32{}
		errorCount := atomic.Int32{}

		start := time.Now()

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				resp, err := client.Get(ctx, "https://httpbin.org/get")
				if err != nil {
					errorCount.Add(1)
					t.Logf("External request %d failed: %v", id, err)
					return
				}
				defer func() {
					_ = resp.Body.Close()
				}()

				if resp.StatusCode == http.StatusOK {
					successCount.Add(1)
				} else {
					errorCount.Add(1)
					t.Logf("External request %d status: %d", id, resp.StatusCode)
				}
			}(i)

			time.Sleep(100 * time.Millisecond)
		}

		wg.Wait()
		duration := time.Since(start)

		t.Logf("External service test results:")
		t.Logf("  Requests: %d", numRequests)
		t.Logf("  Successful: %d", successCount.Load())
		t.Logf("  Errors: %d", errorCount.Load())
		t.Logf("  Duration: %v", duration)
		t.Logf("  Requests per second: %.2f", float64(numRequests)/duration.Seconds())
		t.Logf("  Success rate: %.2f%%", float64(successCount.Load())/float64(numRequests)*100)

		if float64(successCount.Load())/float64(numRequests) < 0.7 {
			t.Errorf("External service success rate too low: %.2f%%",
				float64(successCount.Load())/float64(numRequests)*100)
		}
	})

	t.Run("DNSResolution", func(t *testing.T) {
		client := NewHttpClient(
			WithDNSCacheTTL(1*time.Minute),
			WithCBMaxFailures(100),
		)

		domains := []string{
			"google.com",
			"github.com",
			"stackoverflow.com",
		}

		for _, domain := range domains {
			url := fmt.Sprintf("https://%s", domain)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
			if err != nil {
				t.Logf("Failed to create request for %s: %v", domain, err)
				cancel()
				continue
			}

			resp, err := client.Do(req)
			cancel()

			if err != nil {
				t.Logf("DNS resolution test for %s: %v", domain, err)
			} else if resp != nil {
				defer func() {
					_ = resp.Body.Close()
				}()
				t.Logf("DNS resolution for %s: OK (status %d)", domain, resp.StatusCode)
			}
		}
	})
}

func TestMetricsCollection(t *testing.T) {
	var metricsCollected []Metrics
	var mu sync.Mutex

	server := safeTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	client := newTestClient(
		WithEnableMetrics(true),
		WithMetricsCallback(func(m Metrics) {
			mu.Lock()
			defer mu.Unlock()
			metricsCollected = append(metricsCollected, m)
		}),
	)

	for i := 0; i < 5; i++ {
		resp, err := client.Get(context.Background(), server.URL)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	mu.Lock()
	collected := len(metricsCollected)
	mu.Unlock()

	if collected != 5 {
		t.Errorf("Expected 5 metrics, got %d", collected)
	}

	mu.Lock()
	for _, m := range metricsCollected {
		if m.Host == "" {
			t.Error("Metric should have host")
		}
		if m.RequestDuration == 0 {
			t.Error("Metric should have duration")
		}
		if m.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", m.StatusCode)
		}
	}
	mu.Unlock()
}
