package goserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGzipConcurrentBufferOwnership(t *testing.T) {
	const n = 48
	s := NewServer("0")
	var arrived sync.WaitGroup
	arrived.Add(n)
	gate := make(chan struct{})
	go func() { arrived.Wait(); close(gate) }()
	h := s.GzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := strings.Repeat(r.URL.Query().Get("id"), 25)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(first))
		arrived.Done()
		<-gate
		last := strings.Repeat("z", 2000)
		if count, err := w.Write([]byte(last)); err != nil || count != len(last) {
			t.Errorf("write: %d %v", count, err)
		}
	}))
	var done sync.WaitGroup
	for i := 0; i < n; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			id := fmt.Sprintf("%03d", i)
			r := httptest.NewRequest("GET", "/?id="+id, nil)
			r.Header.Set("Accept-Encoding", "gzip")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			gz, err := gzip.NewReader(w.Body)
			if err != nil {
				t.Error(err)
				return
			}
			defer gz.Close()
			body, err := io.ReadAll(gz)
			if err != nil || string(body) != strings.Repeat(id, 25)+strings.Repeat("z", 2000) {
				t.Errorf("mixed response for %s: %v", id, err)
			}
		}(i)
	}
	done.Wait()
}

func TestIdempotencyPrincipalPayloadAndAuthorization(t *testing.T) {
	s := NewServer("0")
	s.Cache = NewCache(64, "")
	var calls int
	h := s.IdempotencyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Set-Cookie", "private=value")
		_, _ = fmt.Fprintf(w, "result-%d", calls)
	}))
	allowed := true
	authorized := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed {
			http.Error(w, "revoked", 403)
			return
		}
		h.ServeHTTP(w, r)
	})
	request := func(tenant, subject, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/orders", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "same")
		r = r.WithContext(WithIdempotencyScope(r.Context(), tenant, subject))
		w := httptest.NewRecorder()
		authorized.ServeHTTP(w, r)
		return w
	}
	first := request("a", "alice", "one")
	replay := request("a", "alice", "one")
	if replay.Body.String() != first.Body.String() || replay.Header().Get("Idempotent-Replayed") != "true" || calls != 1 {
		t.Fatal("replay failed")
	}
	if replay.Header().Get("Set-Cookie") != "" {
		t.Fatal("replayed cookie")
	}
	if request("a", "alice", "two").Code != 409 || calls != 1 {
		t.Fatal("payload conflict executed")
	}
	if request("a", "bob", "one").Body.String() == first.Body.String() {
		t.Fatal("cross-user replay")
	}
	if request("b", "alice", "one").Body.String() == first.Body.String() {
		t.Fatal("cross-tenant replay")
	}
	if request("a", "", "one").Code != 401 || calls != 3 {
		t.Fatal("missing principal executed")
	}
	allowed = false
	if request("a", "alice", "one").Code != 403 || calls != 3 {
		t.Fatal("cached response bypassed revoked authorization")
	}
}

func TestIdempotencyResponseBound(t *testing.T) {
	w := newResponseInterceptor()
	if _, err := w.Write(bytes.Repeat([]byte("x"), maxIdempotencyResponse+1)); err == nil || w.body.Len() != 0 {
		t.Fatal("unbounded response")
	}
}

func TestIdempotencyConcurrentReplay(t *testing.T) {
	s := NewServer("0")
	s.Cache = NewCache(16, "")
	var calls atomic.Int32
	h := s.IdempotencyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	var ready, done sync.WaitGroup
	const n = 32
	ready.Add(n)
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			r := httptest.NewRequest("POST", "/orders", strings.NewReader("same payload"))
			r.Header.Set("Idempotency-Key", "same-key")
			r = r.WithContext(WithIdempotencyScope(r.Context(), "tenant", "user"))
			ready.Done()
			<-gate
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 201 || w.Body.String() != "created" {
				t.Errorf("replay: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	ready.Wait()
	close(gate)
	done.Wait()
	if calls.Load() != 1 {
		t.Fatalf("executed %d times", calls.Load())
	}
}

func TestShutdownDrainsHTTPBeforeClosingDependencies(t *testing.T) {
	s := NewServer("0")
	entered, release := make(chan struct{}), make(chan struct{})
	var dependencyClosed atomic.Bool
	s.GET("/work", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		if dependencyClosed.Load() {
			t.Error("dependency closed before handler returned")
		}
		_, _ = w.Write([]byte("ok"))
	})
	ts := httptest.NewServer(s.buildHandler(s.Config))
	defer ts.Close()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, err := ts.Client().Get(ts.URL + "/work")
		if err != nil {
			t.Error(err)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || string(body) != "ok" {
			t.Errorf("drain response %q: %v", body, err)
		}
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.shutdown(ctx, ts.Config, func() { dependencyClosed.Store(true) }) }()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-requestDone
	if !dependencyClosed.Load() {
		t.Fatal("dependency not closed")
	}
}

func TestTimeoutRetainsAdmissionUntilHandlerFinishes(t *testing.T) {
	s := NewServer("0")
	s.ResponseMode = "json"
	block := make(chan struct{})
	finished := make(chan struct{})
	var released atomic.Bool
	lease := &workLease{release: func() { released.Store(true) }}
	lease.refs.Store(1)
	h := s.TimeoutMiddleware(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		<-block
		if _, err := w.Write([]byte("late")); err != http.ErrHandlerTimeout {
			t.Errorf("late write: %v", err)
		}
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), workLeaseKey{}, lease))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	lease.done()
	if w.Code != 504 || released.Load() {
		t.Fatal("timeout freed active work")
	}
	close(block)
	<-finished
	s.backgroundWg.Wait()
	if !released.Load() {
		t.Fatal("admission leaked")
	}
}

func TestShutdownWaitsForWorkBeforeDependencies(t *testing.T) {
	s := NewServer("0")
	if !s.beginWork() {
		t.Fatal("admission")
	}
	closed := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.shutdown(ctx, &http.Server{}, func() { close(closed) })
	if err != context.DeadlineExceeded {
		t.Fatalf("deadline: %v", err)
	}
	select {
	case <-closed:
		t.Fatal("closed dependencies during work")
	default:
	}
	if s.beginWork() {
		t.Fatal("admitted work during shutdown")
	}
	s.backgroundWg.Done()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cleanup not completed")
	}
}

func TestShutdownBoundsBlockingHook(t *testing.T) {
	s := NewServer("0")
	release := make(chan struct{})
	finished := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.shutdown(ctx, &http.Server{}, func() { defer close(finished); <-release })
	close(release)
	<-finished
	if err != context.DeadlineExceeded {
		t.Fatalf("deadline: %v", err)
	}
}
