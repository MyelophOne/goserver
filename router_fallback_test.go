package goserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	runtime "github.com/myelophone/goserver/web/runtime"
)

func TestExplicitRouteWinsOverFileBasedFallback(t *testing.T) {
	router := NewRouter()
	router.HandleFallback(http.MethodGet, "/pogoda", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("generated"))
	}))
	router.Handle(http.MethodGet, "/pogoda", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("explicit"))
	}))

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/pogoda", nil))
	if response.Body.String() != "explicit" {
		t.Fatalf("response=%q", response.Body.String())
	}
}

func TestFileBasedServerHandlerReceivesRequestData(t *testing.T) {
	var handler runtime.ServerHandler
	for _, route := range runtime.ServerRoutes() {
		if route.Method == http.MethodGet && route.Path == "/pogoda" {
			handler = route.Handler
			break
		}
	}
	if handler == nil {
		t.Fatal("generated GET /pogoda handler is not registered")
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/pogoda?city=Warsaw", nil)
	if err := handler(&runtime.Event{Writer: response, Request: request}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["query"] != "Warsaw" {
		t.Fatalf("query=%v", body["query"])
	}
}

func TestSharedLayoutScriptUsesClientEntryInsteadOfChunk(t *testing.T) {
	source := "export function mount() {}"
	id := hashText(source)
	app := &App{
		entrySources: map[string]bool{id: true},
		modules:      map[string]string{},
		chunks:       map[string]string{},
		scriptUsage:  map[string]int{id: 1},
		sourceChunks: map[string]map[string]string{},
	}
	plan := app.modulePlan("index.gosh", []ScriptBinding{{Source: source, Target: "#app"}}, false)
	if len(plan.Bindings) != 1 || plan.Bindings[0].ID != id || plan.Bindings[0].Chunk != "" || plan.Bindings[0].Src != "" {
		t.Fatalf("shared layout module plan=%#v", plan)
	}
	if len(app.chunks) != 0 {
		t.Fatalf("shared layout script was emitted as chunk: %#v", app.chunks)
	}
}

func TestSharedLayoutModuleIsRegisteredInClientEntry(t *testing.T) {
	entry, err := BuildClientEntry(map[string]string{"layout": "export function mount() {}"})
	if err != nil {
		t.Skipf("client build tooling is unavailable: %v", err)
	}
	if !strings.Contains(string(entry), "__GOSH_ENTRY_MODULES__") {
		t.Fatalf("shared layout module was not registered in the entry: %s", entry)
	}
	if strings.Contains(string(entry), "__GOSH_ENTRY_SOURCES__") {
		t.Fatalf("client entry must not embed layout source strings: %s", entry)
	}
}

func TestClientEntryModuleRegistryDetection(t *testing.T) {
	if !clientEntrySupportsModuleRegistry([]byte("window.__GOSH_ENTRY_MODULES__ = {};")) {
		t.Fatal("current client entry was not recognised")
	}
	if clientEntrySupportsModuleRegistry([]byte("window.__GOSH_ENTRY_SOURCES__ = {};")) {
		t.Fatal("legacy client entry must not use ID-only module bindings")
	}
}
