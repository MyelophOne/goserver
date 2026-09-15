package goserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestCacheableRequestAllowsFrameworkRuntimeCookie(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.AddCookie(&http.Cookie{Name: runtimeStateCookie, Value: "owner"})
	if !cacheableRequest(request) {
		t.Fatal("framework runtime cookie unexpectedly bypasses route cache")
	}
	request.AddCookie(&http.Cookie{Name: "session_id", Value: "visitor-specific"})
	if cacheableRequest(request) {
		t.Fatal("application session cookie must bypass route cache")
	}
}

func TestServerTimingRequiresExplicitConfiguration(t *testing.T) {
	started := time.Now().Add(-10 * time.Millisecond)
	rendered := started.Add(5 * time.Millisecond)

	withoutTiming := &App{}
	response := httptest.NewRecorder()
	withoutTiming.setServerTiming(response, started, rendered)
	if got := response.Header().Get("Server-Timing"); got != "" {
		t.Fatalf("Server-Timing was sent when disabled: %q", got)
	}

	cfg := logic.RuntimeConfig{}
	cfg.Render.ServerTiming = true
	withTiming := &App{config: cfg}
	response = httptest.NewRecorder()
	withTiming.setServerTiming(response, started, rendered)
	if got := response.Header().Get("Server-Timing"); got == "" {
		t.Fatal("Server-Timing was not sent when explicitly enabled")
	}
}

func TestReplaceRuntimeStateTokens(t *testing.T) {
	source := `<div data-gosh-state="old-a"></div><aside data-gosh-state="keep"></aside><div data-gosh-state="old-b"></div>`
	got := replaceRuntimeStateTokens(source, map[string]string{
		"old-a": "new-a",
		"old-b": "new-b",
	})
	want := `<div data-gosh-state="new-a"></div><aside data-gosh-state="keep"></aside><div data-gosh-state="new-b"></div>`
	if got != want {
		t.Fatalf("replaceRuntimeStateTokens() = %q, want %q", got, want)
	}
}

func TestReplaceRuntimeStateTokensLeavesMalformedMarkupUntouched(t *testing.T) {
	source := `<div data-gosh-state="old-a>`
	if got := replaceRuntimeStateTokens(source, map[string]string{"old-a": "new-a"}); got != source {
		t.Fatalf("replaceRuntimeStateTokens() = %q, want original malformed markup", got)
	}
}

func TestCompactHTMLIntertagWhitespace(t *testing.T) {
	source := []byte("<!DOCTYPE html>\n<html>\n  <head>\n\t<meta charset=\"UTF-8\">\n  </head>\n  <body><p>Hello</p>\n  <p>World</p></body>\n</html>")
	want := `<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body><p>Hello</p><p>World</p></body></html>`
	if got := string(compactHTMLIntertagWhitespace(source)); got != want {
		t.Fatalf("compactHTMLIntertagWhitespace() = %q, want %q", got, want)
	}
}

func TestCompactHTMLIntertagWhitespaceCompactsRawText(t *testing.T) {
	source := []byte("<pre>  first\n  second </pre>\n<script>const compare = a >\n  < b;</script>\n<div>ok</div>")
	want := "<pre>  first   second </pre><script>const compare = a >   < b;</script><div>ok</div>"
	if got := string(compactHTMLIntertagWhitespace(source)); got != want {
		t.Fatalf("compactHTMLIntertagWhitespace() = %q, want %q", got, want)
	}
}

func TestExactRootRouteRuleDoesNotMatchOtherPages(t *testing.T) {
	app := &App{config: logic.RuntimeConfig{RouteRules: map[string]logic.RouteRule{
		"/": {Cache: &logic.CachePolicy{MaxAge: 10}, SWR: 10},
	}}}
	if _, ok := app.routeRule("/"); !ok {
		t.Fatal("root route rule did not match the root page")
	}
	for _, path := range []string{"/test", "/images", "/nested/page"} {
		if _, ok := app.routeRule(path); ok {
			t.Fatalf("exact root route rule unexpectedly matched %q", path)
		}
	}
}

func TestRouteTTLFallsBackToSWR(t *testing.T) {
	ttl, swr := routeTTL(logic.RouteRule{SWR: 10})
	if ttl != 10 || swr != 10 {
		t.Fatalf("routeTTL(SWR-only) = (%d, %d), want (10, 10)", ttl, swr)
	}
}

func TestComponentHasRuntimeAction(t *testing.T) {
	static := []Node{&ElementNode{Tag: "button", Attrs: []Attribute{{Name: "type", Value: "button"}}}}
	if componentHasRuntimeAction(static) {
		t.Fatal("ordinary static component unexpectedly needs runtime state")
	}
	action := []Node{&ElementNode{Tag: "button", Attrs: []Attribute{{Name: "@click", Value: "save", Event: true}}}}
	if !componentHasRuntimeAction(action) {
		t.Fatal("event directive must retain runtime state")
	}
}
