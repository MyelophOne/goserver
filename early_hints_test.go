package goserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type informationalResponseWriter struct {
	header   http.Header
	statuses []int
	links103 []string
}

func (w *informationalResponseWriter) Header() http.Header { return w.header }

func (w *informationalResponseWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	if status == http.StatusEarlyHints {
		w.links103 = append([]string(nil), w.header.Values("Link")...)
	}
}

func (w *informationalResponseWriter) Write(data []byte) (int, error) { return len(data), nil }

func TestResponseWritersDoNotTreatEarlyHintsAsFinal(t *testing.T) {
	tests := []struct {
		name string
		wrap func(http.ResponseWriter) (http.ResponseWriter, func())
	}{
		{
			name: "status",
			wrap: func(w http.ResponseWriter) (http.ResponseWriter, func()) {
				return newStatusWriter(w), func() {}
			},
		},
		{
			name: "timeout",
			wrap: func(w http.ResponseWriter) (http.ResponseWriter, func()) {
				return newTimeoutWriter(w), func() {}
			},
		},
		{
			name: "gzip",
			wrap: func(w http.ResponseWriter) (http.ResponseWriter, func()) {
				gzip := &gzipResponseWriter{ResponseWriter: w}
				return gzip, func() { _ = gzip.Close() }
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture := &informationalResponseWriter{header: make(http.Header)}
			writer, closeWriter := test.wrap(capture)
			writer.Header().Add("Link", "</_gosh/style/app.css>; rel=preload; as=style")
			writer.WriteHeader(http.StatusEarlyHints)
			writer.Header().Del("Link")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("ok"))
			closeWriter()

			if len(capture.links103) != 1 {
				t.Fatalf("103 Link headers = %v", capture.links103)
			}
			if links := capture.header.Values("Link"); len(links) != 0 {
				t.Fatalf("final Link headers = %v, want none", links)
			}
			if len(capture.statuses) < 2 || capture.statuses[0] != http.StatusEarlyHints || capture.statuses[1] != http.StatusOK {
				t.Fatalf("statuses = %v, want 103 then 200", capture.statuses)
			}
		})
	}
}

func TestIsSameOriginNavigation(t *testing.T) {
	tests := []struct {
		name    string
		referer string
		want    bool
	}{
		{name: "initial request", want: false},
		{name: "same origin", referer: "http://example.test/products", want: true},
		{name: "external origin", referer: "https://search.example/products", want: false},
		{name: "invalid referer", referer: "://broken", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.test/catalog", nil)
			if test.referer != "" {
				r.Header.Set("Referer", test.referer)
			}
			if got := isSameOriginNavigation(r); got != test.want {
				t.Fatalf("isSameOriginNavigation() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRequestWantsConnectionClose(t *testing.T) {
	tests := []struct {
		name       string
		connection string
		close      bool
		want       bool
	}{
		{name: "keep alive", connection: "keep-alive"},
		{name: "connection header", connection: "close", want: true},
		{name: "connection token", connection: "upgrade, close", want: true},
		{name: "parsed close", close: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.test/catalog", nil)
			r.Header.Set("Connection", test.connection)
			r.Close = test.close
			if got := requestWantsConnectionClose(r); got != test.want {
				t.Fatalf("requestWantsConnectionClose() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSendEarlyHintsSkipsConnectionClose(t *testing.T) {
	app := &App{}
	app.config.Render.EarlyHints = true
	writer := &informationalResponseWriter{header: make(http.Header)}
	r := httptest.NewRequest(http.MethodGet, "http://example.test/catalog", nil)
	r.Header.Set("Connection", "close")
	app.sendEarlyHints(writer, r, "/_gosh/runtime.js", []string{"/_gosh/style/app.css"})
	if len(writer.statuses) != 0 {
		t.Fatalf("statuses = %v, want no informational response", writer.statuses)
	}
}

func TestAddNonceToTrustedHTMLAddsNonceToExternalScript(t *testing.T) {
	body := addNonceToTrustedHTML([]byte(`<script src="/_gosh/entry/app.js" defer></script>`), "nonce-value")
	if !strings.Contains(string(body), `<script nonce="nonce-value" src="/_gosh/entry/app.js"`) {
		t.Fatalf("external script lacks nonce: %s", body)
	}
	if !strings.Contains(string(body), `nonce="nonce-value"`) {
		t.Fatalf("external script nonce was not retained: body=%s", body)
	}
}

func TestCSPAllowsSameOriginRuntimeScriptElement(t *testing.T) {
	writer := httptest.NewRecorder()
	setCSPHeaders(writer, "nonce-value")
	policy := writer.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src-elem 'self' 'nonce-nonce-value'") {
		t.Fatalf("CSP does not allow the nonce-bound runtime script: %q", policy)
	}
}

func TestPublicStaticCSPBlocksScripts(t *testing.T) {
	writer := httptest.NewRecorder()
	setPublicStaticCSPHeaders(writer)
	if policy := writer.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "script-src 'none'") {
		t.Fatalf("public-static CSP allows scripts: %q", policy)
	}
}
