//go:build !myelophone_prod

package goserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestEnableWebServesFileBasedPage(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `id=app`) {
		t.Fatal("web page did not contain the SSR application root")
	}
	if !strings.Contains(response.Body.String(), `src=/_gosh/entry/`) {
		t.Fatal("web page did not use the compact runtime URL")
	}
	if strings.Contains(response.Body.String(), "/_m/") {
		t.Fatal("legacy runtime URL leaked into the page")
	}
	if strings.Contains(response.Body.String(), "data-gosh-props") {
		t.Fatal("SSR props leaked into the browser markup")
	}
	if !strings.Contains(response.Body.String(), `id="gosh-page-runtime"`) || !strings.Contains(response.Body.String(), `"state":`) {
		t.Fatal("page runtime metadata did not receive a server state token")
	}
	if strings.Contains(response.Body.String(), "data-gosh-state=") || strings.Contains(response.Body.String(), "data-gosh-modules=") {
		t.Fatal("application root leaked runtime metadata into data attributes")
	}
	if !strings.Contains(response.Header().Get("Set-Cookie"), runtimeStateCookie+"=") {
		t.Fatal("application response did not bind runtime state to a cookie")
	}
	if strings.ContainsAny(response.Body.String(), "\r\n") {
		t.Fatal("web document was not minified to one line")
	}
}

func TestSiteSearchStreamsCompactDocuments(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/_gosh/site-search", nil)
	request.Header.Set("X-GOSH-Site-Search-Rebuild", "1")
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "application/x-ndjson") {
		t.Fatalf("content-type=%q", got)
	}
	line := strings.Split(strings.TrimSpace(response.Body.String()), "\n")[0]
	if !strings.Contains(line, `"path":"/"`) || !strings.Contains(line, `"content":`) {
		t.Fatalf("stream does not contain a compact searchable document: %s", line)
	}
	if strings.Contains(line, "nonce=") || strings.Contains(line, "<html") {
		t.Fatalf("stream leaked browser document markup: %s", line)
	}
}

func TestServerSiteSearchHonorsAndOr(t *testing.T) {
	app := &App{config: logic.RuntimeConfig{}, siteSearchIndex: []siteSearchDocument{
		{Path: "/", Title: "Alpha", Content: "alpha beta"},
		{Path: "/only-alpha", Title: "Alpha only", Content: "alpha"},
	}}
	app.config.SiteSearch.ServerSearch = true
	for _, test := range []struct{ query, want string }{{"q=alpha+beta&operator=and", `"path":"/"`}, {"q=alpha+beta&operator=or", `"path":"/only-alpha"`}} {
		response := httptest.NewRecorder()
		app.siteSearchQueryHandler(response, httptest.NewRequest(http.MethodGet, "http://example.test/_gosh/site-search/query?"+test.query, nil))
		if !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("query %q results=%s", test.query, response.Body.String())
		}
	}
}

func TestSiteSearchQueryDoesNotRedirectForQueryOrder(t *testing.T) {
	s := NewServer("0")
	h := s.SanitizeURLMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/_gosh/site-search/query?q=test&limit=10", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestCursorTeleportAndComponentAreDiscoverable(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatalf("load components: %v", err)
	}
	if !components.Has("UiCursorCreative") {
		t.Fatalf("UiCursorCreative is absent; available names: %v", components.Names())
	}
	teleports, err := LoadComponents(systemTeleportDir)
	if err != nil {
		t.Fatalf("load teleports: %v", err)
	}
	if !teleports.Has("CursorCreative") {
		t.Fatalf("CursorCreative teleport is absent; available names: %v", teleports.Names())
	}
	cursor, _ := teleports.Get("CursorCreative")
	components.items[normalizeComponentLookup(cursor.Name)] = cursor
	components.names = componentNames(components.items)
	rendered, err := NewRenderer(components, "").RenderComponentContext(cursor, Props{}, &logic.Context{})
	if err != nil {
		t.Fatalf("render CursorCreative teleport: %v", err)
	}
	if !strings.Contains(rendered.HTML, "data-ui-cursor") {
		t.Fatalf("cursor markup was not rendered: %s", rendered.HTML)
	}
}

func TestTeleportIncludesLazyComponentStyles(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatalf("load components: %v", err)
	}
	teleports, err := LoadComponents(systemTeleportDir)
	if err != nil {
		t.Fatalf("load teleports: %v", err)
	}
	for _, name := range teleports.Names() {
		component, _ := teleports.Get(name)
		components.items[normalizeComponentLookup(name)] = component
	}
	components.names = componentNames(components.items)

	result := RenderResult{}
	if err := appendTeleports(&result, &logic.Context{}, NewRenderer(components, ""), teleports); err != nil {
		t.Fatalf("append teleports: %v", err)
	}
	if !strings.Contains(strings.Join(result.CSSParts, "\n"), "[data-ui-cursor-dot]") {
		t.Fatalf("lazy CursorCreative styles are absent from teleport result: %q", result.CSSParts)
	}
}

func BenchmarkRenderTestPage(b *testing.B) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		b.Fatal(err)
	}
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		b.Fatal(err)
	}
	page, ok := pages.ByRelative("test.gosh")
	if !ok {
		b.Fatal("test.gosh page is absent")
	}
	renderer := NewRenderer(components)
	ctx := &logic.Context{Path: "/test", Params: map[string]any{}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := renderer.RenderComponentContext(page.View, Props{}, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderTestPageWithLayoutAndTeleport(b *testing.B) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		b.Fatal(err)
	}
	layouts, err := LoadComponents(systemLayoutsDir)
	if err != nil {
		b.Fatal(err)
	}
	teleports, err := LoadComponents(systemTeleportDir)
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range teleports.Names() {
		component, _ := teleports.Get(name)
		components.items[normalizeComponentLookup(name)] = component
	}
	components.names = componentNames(components.items)
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		b.Fatal(err)
	}
	page, ok := pages.ByRelative("test.gosh")
	if !ok {
		b.Fatal("test.gosh page is absent")
	}
	layout, ok := layouts.Get("default")
	if !ok {
		b.Fatal("default layout is absent")
	}
	renderer := NewRenderer(components)
	ctx := &logic.Context{Path: "/test", Params: map[string]any{}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := renderer.RenderComponentContext(page.View, Props{}, ctx)
		if err != nil {
			b.Fatal(err)
		}
		result, err = renderer.RenderLayout(layout, result, ctx)
		if err != nil {
			b.Fatal(err)
		}
		if err := appendTeleports(&result, ctx, renderer, teleports); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderTestPageParallel(b *testing.B) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		b.Fatal(err)
	}
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		b.Fatal(err)
	}
	page, ok := pages.ByRelative("test.gosh")
	if !ok {
		b.Fatal("test.gosh page is absent")
	}
	renderer := NewRenderer(components)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := &logic.Context{Path: "/test", Params: map[string]any{}}
		for pb.Next() {
			if _, err := renderer.RenderComponentContext(page.View, Props{}, ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestBindRuntimeStateReissuesTeleportTokens(t *testing.T) {
	old := "teleport-state"
	app := &App{config: logic.RuntimeConfig{Environment: "development"}, runtimeState: newRuntimeStateStore()}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	response := httptest.NewRecorder()
	result := RenderResult{
		TeleportHTML:  `<gosh-component data-gosh-state="` + old + `"></gosh-component>`,
		RuntimeStates: map[string]Props{old: {"teleport": true}},
	}
	if _, err := app.bindRuntimeState(response, request, &result); err != nil {
		t.Fatalf("bind runtime state: %v", err)
	}
	if strings.Contains(result.TeleportHTML, `data-gosh-state="`+old+`"`) {
		t.Fatalf("teleport state token was not reissued: %s", result.TeleportHTML)
	}
}

func TestRouteSWRUsesFullDocumentTemplateWithFreshPrivateValues(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	app := s.Web()
	app.config.RouteRules = map[string]logic.RouteRule{
		"/": {Cache: &logic.CachePolicy{MaxAge: 60}, SWR: 10},
	}

	first := httptest.NewRecorder()
	s.router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	if first.Code != http.StatusOK || first.Header().Get("X-MyelophOne-Cache") != "miss" {
		t.Fatalf("first response: status=%d cache=%q", first.Code, first.Header().Get("X-MyelophOne-Cache"))
	}
	if strings.Contains(first.Body.String(), cachedDocumentNonceMarker) || strings.Contains(first.Body.String(), cachedDocumentRootMarker) {
		t.Fatal("first response leaked a cached-document marker")
	}

	second := httptest.NewRecorder()
	s.router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	if second.Code != http.StatusOK || second.Header().Get("X-MyelophOne-Cache") != "hit" {
		t.Fatalf("second response: status=%d cache=%q", second.Code, second.Header().Get("X-MyelophOne-Cache"))
	}
	if strings.Contains(second.Body.String(), cachedDocumentNonceMarker) || strings.Contains(second.Body.String(), cachedDocumentRootMarker) {
		t.Fatal("cached response leaked a document marker")
	}
	if first.Header().Get("Content-Security-Policy") == second.Header().Get("Content-Security-Policy") {
		t.Fatal("cached response reused the CSP nonce")
	}
	for _, response := range []*httptest.ResponseRecorder{first, second} {
		policy := response.Header().Get("Content-Security-Policy")
		prefix := "script-src 'nonce-"
		start := strings.Index(policy, prefix)
		if start < 0 {
			t.Fatalf("CSP has no script nonce: %q", policy)
		}
		nonce := policy[start+len(prefix):]
		if end := strings.IndexByte(nonce, '\''); end >= 0 {
			nonce = nonce[:end]
		}
		if !strings.Contains(response.Body.String(), `nonce="`+nonce+`"`) {
			t.Fatalf("runtime document lacks CSP nonce %q", nonce)
		}
	}
	entry, ok := app.routeCache.get(routeRenderCacheKey(httptest.NewRequest(http.MethodGet, "http://example.test/", nil)))
	if !ok || entry.Document == nil {
		t.Fatal("route cache does not retain a full-document template")
	}
}

func TestRouteSWROnlyCachesPage(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	app := s.Web()
	app.config.RouteRules = map[string]logic.RouteRule{
		"/": {SWR: 10},
	}

	first := httptest.NewRecorder()
	s.router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	if first.Code != http.StatusOK || first.Header().Get("X-MyelophOne-Cache") != "miss" {
		t.Fatalf("first response: status=%d cache=%q", first.Code, first.Header().Get("X-MyelophOne-Cache"))
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	for _, cookie := range first.Result().Cookies() {
		secondRequest.AddCookie(cookie)
	}
	s.router.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusOK || second.Header().Get("X-MyelophOne-Cache") != "hit" {
		t.Fatalf("SWR-only route was not cached: status=%d cache=%q", second.Code, second.Header().Get("X-MyelophOne-Cache"))
	}
}

func TestReplaceCachedDocumentMarkers(t *testing.T) {
	body := []byte("before __GOSH_ROOT_STATE__ middle __GOSH_CSP_NONCE__ after __GOSH_ROOT_STATE__")
	got := string(replaceCachedDocumentMarkers(body, map[string]string{
		cachedDocumentRootMarker:  "state-token",
		cachedDocumentNonceMarker: "nonce-value",
	}))
	want := "before state-token middle nonce-value after state-token"
	if got != want {
		t.Fatalf("replaceCachedDocumentMarkers() = %q, want %q", got, want)
	}
}

func TestPublicStaticRouteCachingPolicy(t *testing.T) {
	rule := logic.RouteRule{Cache: &logic.CachePolicy{MaxAge: 10}, SWR: 10, PublicStatic: true}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.Header.Set("Cookie", "session=visitor-specific")
	if !cacheablePageRequest(request, rule.PublicStatic) {
		t.Fatal("explicit public-static route should be cacheable with cookies")
	}
	request.Header.Set("Authorization", "Bearer secret")
	if cacheablePageRequest(request, rule.PublicStatic) {
		t.Fatal("authorized request must bypass public-static cache")
	}
	if got, want := publicCacheControl(rule), "public, max-age=10, s-maxage=10, stale-while-revalidate=10"; got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
}

func TestRouteRuleExclusionsFallBackToLessSpecificRule(t *testing.T) {
	app := &App{config: logic.RuntimeConfig{RouteRules: map[string]logic.RouteRule{
		"/**":          {Cache: &logic.CachePolicy{MaxAge: 5}},
		"/products/*":  {Cache: &logic.CachePolicy{MaxAge: 25}, Exclude: []string{"/products/15", "*preview*"}},
		"/products/15": {Cache: &logic.CachePolicy{MaxAge: 90}},
	}}}
	if rule, ok := app.routeRule("/products/9"); !ok || rule.Cache == nil || rule.Cache.MaxAge != 25 {
		t.Fatalf("product rule = %#v, found=%t", rule, ok)
	}
	if rule, ok := app.routeRule("/products/15"); !ok || rule.Cache == nil || rule.Cache.MaxAge != 90 {
		t.Fatalf("exact override = %#v, found=%t", rule, ok)
	}
	if rule, ok := app.routeRule("/products/preview/9"); !ok || rule.Cache == nil || rule.Cache.MaxAge != 5 {
		t.Fatalf("excluded rule should fall back = %#v, found=%t", rule, ok)
	}
}

func TestRenderBasePublicStaticOmitsRuntimeStateAndScripts(t *testing.T) {
	base, err := loadBaseTemplate()
	if err != nil {
		t.Fatalf("loadBaseTemplate: %v", err)
	}
	app := &App{base: base, config: logic.RuntimeConfig{}}
	body, err := app.renderBase(
		RenderResult{HTML: `<main data-gosh-state="private-token">static</main>`, Data: Props{"title": "Static"}},
		&Page{RelativePath: "pages/pricing.gosh"}, http.StatusOK, "", "", true,
	)
	if err != nil {
		t.Fatalf("renderBase public static: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "data-gosh-state") || strings.Contains(text, "_gosh/entry") || strings.Contains(text, "<script") {
		t.Fatalf("public-static document contains runtime state or scripts: %s", text)
	}
	if !strings.Contains(text, `data-gosh-public-static=1`) {
		t.Fatalf("public-static marker absent: %s", text)
	}
}

func TestMissingDefaultLayoutUsesSlotOnlyFallback(t *testing.T) {
	layouts := &Components{items: map[string]*Component{}}
	ensureDefaultLayout(layouts, "default")
	layout, ok := layouts.Get("default")
	if !ok {
		t.Fatal("missing generated default layout")
	}
	page := RenderResult{HTML: `<main id="main">page</main>`, Data: Props{"title": "Page"}}
	rendered, err := NewRenderer(&Components{items: map[string]*Component{}}).RenderLayout(layout, page, nil)
	if err != nil || rendered.HTML != page.HTML {
		t.Fatalf("slot-only fallback: html=%q err=%v", rendered.HTML, err)
	}
}

func TestUiIconRendersPreparedIconifyAsset(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatalf("load components: %v", err)
	}
	result, err := NewRenderer(components).Render("UiIcon", Props{"name": "mdi:home", "size": "24"})
	if err != nil {
		t.Fatalf("render icon: %v", err)
	}
	if !strings.Contains(result.HTML, `data-ui-icon`) || !strings.Contains(result.HTML, `data-ui-icon-src="https://api.iconify.design/mdi/home.svg"`) || !strings.Contains(result.HTML, `data-ui-icon-size="24px"`) {
		t.Fatalf("unexpected icon markup: %s", result.HTML)
	}
}

func TestUiIcon8RendersPreparedIcons8Asset(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatalf("load components: %v", err)
	}
	result, err := NewRenderer(components).Render("UiIcon8", Props{"icon": "github", "type": "ios-filled", "size": "28", "color": "ffffff"})
	if err != nil {
		t.Fatalf("render Icons8 icon: %v", err)
	}
	if !strings.Contains(result.HTML, `<img`) || !strings.Contains(result.HTML, `src="https://img.icons8.com/ios-filled/28/ffffff/github.png"`) || !strings.Contains(result.HTML, `width="28"`) || !strings.Contains(result.HTML, `loading="lazy"`) {
		t.Fatalf("unexpected Icons8 markup: %s", result.HTML)
	}
}

func TestUiButtonRendersLeadingIcon(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatalf("load components: %v", err)
	}
	result, err := NewRenderer(components).Render("UiButton", Props{"label": "Save", "leadingIcon": "heroicons:check"})
	if err != nil {
		t.Fatalf("render button: %v", err)
	}
	if !strings.Contains(result.HTML, `https://api.iconify.design/heroicons/check.svg`) || !strings.Contains(result.HTML, `data-ui-icon-key=`) {
		t.Fatalf("leading icon is absent from button markup: %s", result.HTML)
	}
}

func TestEnableWebUsesTheExistingServerListener(t *testing.T) {
	s := NewServer("127.0.0.1:9123")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	if s.addr != "127.0.0.1:9123" || s.srv.Addr != "127.0.0.1:9123" {
		t.Fatalf("web changed the goserver listener: addr=%q httpAddr=%q", s.addr, s.srv.Addr)
	}
}

func TestExplicitRouteOverridesWebServerAPI(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	s.GET("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		s.RespondText(w, "explicit route")
	})

	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/api/hello", nil))
	if response.Code != http.StatusOK || response.Body.String() != "explicit route" {
		t.Fatalf("explicit route lost priority: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWebUsesSharedAssetsDirectory(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}

	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/assets/seo/seo-cover.svg", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("shared asset status=%d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "<svg") {
		t.Fatal("shared asset was not served from assets/")
	}
}

func TestRuntimePersistsScrollAcrossReload(t *testing.T) {
	source := string(embeddedRuntimeJS)
	for _, expected := range []string{
		`window.addEventListener("pagehide"`,
		`window.addEventListener("pageshow"`,
		`bfcache-restore`,
		`Runtime.prototype.restoreInitialScroll`,
		`Runtime.prototype.updateScrollHistory`,
		`saveScroll: false`,
		`sessionStorage.setItem("gosh:scroll:"`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("runtime is missing reload scroll restoration: %s", expected)
		}
	}
}

func TestRuntimeProvidesModernFallbackAndIslandScheduler(t *testing.T) {
	source := string(embeddedRuntimeJS)
	for _, expected := range []string{
		`function supportsRuntime()`,
		`document.startViewTransition`,
		`Runtime.prototype.renderNavigation`,
		`targetURL.pathname.replace(/\/+$/, "")`,
		`typeof IntersectionObserver !== "function"`,
		`data-gosh-island`,
		`lazyObservedRoots`,
		`node.firstElementChild || node`,
		`prefetchMaxConcurrent`,
		`cache-invalidate`,
		`Runtime.prototype.useForm`,
		`data-gosh-form`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("runtime is missing %s", expected)
		}
	}
}

func TestGlobalPreloaderUsesRuntimeLoadingStore(t *testing.T) {
	source := string(embeddedRuntimeJS)
	for _, expected := range []string{
		`Runtime.prototype.beginLoading`,
		`Runtime.prototype.endLoading`,
		`Runtime.prototype.withLoading`,
		`window._gosh.useLoading`,
		`"gosh-preloader"`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("runtime is missing global loading behavior: %s", expected)
		}
	}
	base, err := os.ReadFile("base.html")
	if err != nil {
		t.Fatalf("read base template: %v", err)
	}
	if !strings.Contains(string(base), `id="gosh-preloader"`) || strings.LastIndex(string(base), `id="gosh-preloader"`) < strings.LastIndex(string(base), `{{.PageScripts}}`) {
		t.Fatal("global preloader must be appended after page content and scripts")
	}
}

func TestSPAPageSwapPreservesTeleportModules(t *testing.T) {
	source := string(embeddedRuntimeJS)
	pageBegin := `if (frame.scope === "page") {
                await this.unmountPageModule();
                // Teleports live outside #app`
	start := strings.Index(source, pageBegin)
	if start < 0 {
		t.Fatal("runtime page begin lifecycle was not found")
	}
	segment := source[start : start+500]
	if !strings.Contains(segment, `await this.unmountLooseWithin("#app");`) {
		t.Fatal("SPA navigation must only unmount loose modules inside #app")
	}
	if strings.Contains(segment, "unmountAllLooseModules") {
		t.Fatal("SPA navigation must preserve persistent teleport modules")
	}
	pageUnmount := `Runtime.prototype.unmountPageModule = async function () {`
	start = strings.Index(source, pageUnmount)
	if start < 0 {
		t.Fatal("runtime page-module cleanup lifecycle was not found")
	}
	segment = source[start : start+1200]
	if !strings.Contains(segment, `!app.contains(element)`) || !strings.Contains(segment, `this.looseModules.set(key, persistent)`) {
		t.Fatal("page modules mounted outside #app must be retained as persistent modules")
	}
	cleanup := strings.Index(segment, `this.cleanupModuleEntry(entry, "page module")`)
	preserve := strings.Index(segment, `!app.contains(element)`)
	if cleanup < 0 || preserve < 0 || preserve > cleanup {
		t.Fatal("teleport ownership must be checked before page-module cleanup")
	}
}

func TestClientComponentUsesVisibleIslandByDefault(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	if !strings.Contains(response.Body.String(), `data-gosh-island=visible`) {
		t.Fatalf("client component did not receive the default visible island: %q", response.Body.String())
	}
}

func TestRenderBaseIncludesRuntimeConfiguration(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	body := response.Body.String()
	if !strings.Contains(body, `id=gosh-runtime-config`) {
		t.Fatalf("runtime config was not rendered: %q", body)
	}
	if strings.Contains(body, `webVitals`) || strings.Contains(body, `/_gosh/vitals/`) {
		t.Fatalf("disabled vitals leaked into runtime config: %q", body)
	}
}

func TestDisabledWebVitalsChunkIsNotServed(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	if s.Web().vitalsURL != "" {
		t.Fatalf("disabled vitals URL=%q", s.Web().vitalsURL)
	}
}

func TestGeneratedStylesheetStartsWithCopyrightBanner(t *testing.T) {
	app := &App{styles: map[string]string{}}
	url := app.registerStyle(".card{display:grid}")
	response := httptest.NewRecorder()
	app.styleHandler(response, httptest.NewRequest(http.MethodGet, "http://example.test"+url, nil))
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Body.String(), generatedAssetBanner) {
		t.Fatalf("generated stylesheet banner: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWebSocketComposableChunkIsServedSeparately(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test"+s.Web().webSocketChunkURL, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(response.Body.String(), "useWebSocket") {
		t.Fatalf("websocket chunk: status=%d body=%q", response.Code, response.Body.String())
	}
	if !strings.Contains(string(embeddedRuntimeJS), "Runtime.prototype.useWebSocket") {
		t.Fatal("runtime does not expose the WebSocket composable")
	}
}

func TestUnknownGoshAssetDoesNotFallBackToHTML(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/_gosh/vitals/missing.js", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("unknown GOSH asset must be a JavaScript 404, got status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	if strings.Contains(response.Body.String(), `id="app"`) {
		t.Fatal("unknown GOSH asset fell back to the SSR document")
	}
}

func TestWebContentMarkdownAndCanonicalURL(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/post/example", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Example post") || !strings.Contains(response.Body.String(), "site-shell") {
		t.Fatalf("markdown post: status=%d body=%q", response.Code, response.Body.String())
	}
	redirect := httptest.NewRecorder()
	s.router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "http://example.test/post/example/?draft=0", nil))
	if redirect.Code != http.StatusMovedPermanently || redirect.Header().Get("Location") != "/post/example?draft=0" {
		t.Fatalf("canonical redirect: status=%d location=%q", redirect.Code, redirect.Header().Get("Location"))
	}
	stream := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/post/example", nil)
	request.Header.Set("X-Runtime", "1")
	request.Header.Set("X-GOSH-Runtime", "navigate")
	request.Header.Set("Accept", "application/x-ndjson")
	s.router.ServeHTTP(stream, request)
	if contentType := stream.Header().Get("Content-Type"); !strings.Contains(contentType, "application/x-ndjson") || !strings.Contains(stream.Body.String(), `"type":"html"`) {
		t.Fatalf("content runtime stream: content-type=%q body=%q", contentType, stream.Body.String())
	}
}

func TestRuntimePageCacheRequiresRouteRule(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.Header.Set("X-Runtime", "1")
	request.Header.Set("X-GOSH-Runtime", "navigate")
	request.Header.Set("Accept", "application/x-ndjson")
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Header().Get("X-Runtime-Cache-TTL") != "0" || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("uncached route headers: ttl=%q cache-control=%q", response.Header().Get("X-Runtime-Cache-TTL"), response.Header().Get("Cache-Control"))
	}

	s.Web().config.RouteRules["/"] = logic.RouteRule{Cache: &logic.CachePolicy{MaxAge: 45}}
	request = httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.Header.Set("X-Runtime", "1")
	request.Header.Set("X-GOSH-Runtime", "navigate")
	request.Header.Set("Accept", "application/x-ndjson")
	response = httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Header().Get("X-Runtime-Cache-TTL") != "45" || strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("cached route headers: ttl=%q cache-control=%q", response.Header().Get("X-Runtime-Cache-TTL"), response.Header().Get("Cache-Control"))
	}
}

func TestWebSharedCacheUsesCacheStoreAndTagInvalidation(t *testing.T) {
	shared := NewCache(16, "")
	s := NewServer("0")
	s.Cache = shared
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	if s.Web().webCache != shared {
		t.Fatal("EnableWeb did not attach Server.Cache to the web application")
	}

	app := s.Web()
	result := RenderResult{HTML: "<main>cached</main>", CacheTags: []string{"product:42"}}
	key := "test:shared-render"
	app.sharedRenderSet(context.Background(), key, result, 60, 30)

	got, state, found := app.sharedRenderGet(context.Background(), key, 60, 30)
	if !found || state != "shared-hit" || got.HTML != result.HTML {
		t.Fatalf("shared cache result: found=%t state=%q result=%+v", found, state, got)
	}

	app.invalidateSharedRenderTags(context.Background(), result.CacheTags)
	if _, _, found := app.sharedRenderGet(context.Background(), key, 60, 30); found {
		t.Fatal("tag invalidation must make the shared render cache entry unavailable")
	}

	otherInstance := &App{webCache: shared, routeCache: newRenderCache(4)}
	otherInstance.setRouteCache(context.Background(), key, result)
	entry, found := otherInstance.routeCache.get(key)
	if !found || !otherInstance.localRenderCacheValid(context.Background(), entry) {
		t.Fatal("new local cache entry should be valid")
	}
	app.invalidateSharedRenderTags(context.Background(), result.CacheTags)
	if otherInstance.localRenderCacheValid(context.Background(), entry) {
		t.Fatal("shared tag invalidation must also invalidate an L1 entry in another instance")
	}
}

func TestWebUsesEmbeddedBaseAndMissingValuesAreSafe(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	for _, target := range []string{"/myelophone", "/missing-page"} {
		response := httptest.NewRecorder()
		s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test"+target, nil))
		body := response.Body.String()
		if response.Code != http.StatusOK && response.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%q", target, response.Code, body)
		}
		for _, element := range []string{"<!doctype html>", "<head>", "<body>", "</body>", "</html>"} {
			if !strings.Contains(strings.ToLower(body), element) {
				t.Fatalf("%s is missing %s: %q", target, element, body)
			}
		}
	}
}

func TestWebOptionalSPALoadingTemplate(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	s.Web().spaLoadingTemplate = loadSPALoadingTemplate(true)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `id=spa-loader`) {
		t.Fatalf("SPA loading template: status=%d body=%q", response.Code, response.Body.String())
	}
	if !strings.Contains(string(embeddedRuntimeJS), "removeSPALoader") {
		t.Fatal("runtime does not remove the SPA loading template")
	}
}

func TestWebLocaleRoutes(t *testing.T) {
	a, err := NewWebApp()
	if err != nil {
		t.Fatalf("NewWebApp: %v", err)
	}
	defaultLocale := a.config.DefaultLocale
	if defaultLocale == "" {
		defaultLocale = "en"
	}
	a.config.Locales = []string{defaultLocale}
	a.config.DefaultLocale = defaultLocale
	page, params, status := a.selectPage("/")
	if status != http.StatusOK || page == nil || page.RelativePath != "index.gosh" || params["locale"] != nil {
		t.Fatalf("default localized index: page=%#v params=%#v status=%d", page, params, status)
	}
	page, params, status = a.selectPage("/missing")
	if page != a.pages.NotFound || status != http.StatusNotFound || params["locale"] != nil {
		t.Fatalf("default 404 must use the custom page: page=%#v params=%#v status=%d", page, params, status)
	}
}

func TestPageLayoutOverridesDefaultLayout(t *testing.T) {
	a, err := NewWebApp()
	if err != nil {
		t.Fatalf("NewWebApp: %v", err)
	}
	page, ok := a.pages.ByRelative("index.gosh")
	if !ok || page.Layout != "default" {
		t.Fatalf("page layout directive was not loaded: page=%#v", page)
	}
}

func TestWebDynamicUserRoute(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/user/15", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "User 15") || !strings.Contains(response.Body.String(), `ctx.Param(`) {
		t.Fatalf("dynamic route: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWebResponsesContributeToServerStatusStats(t *testing.T) {
	s := NewServer("0")
	if err := s.EnableWeb(); err != nil {
		t.Fatal(err)
	}
	s.GET("/web-metrics-error", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expected", http.StatusInternalServerError)
	})
	handler := s.buildHandler(s.Config)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/no-such-web-page", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/web-metrics-error", nil))
	stats := s.GetStats()
	if stats.TotalRequests != 2 || stats.Errors4xx != 1 || stats.Errors5xx != 1 {
		t.Fatalf("web status metrics: %#v", stats)
	}
}

func TestWebI18nPageUsesServerTranslations(t *testing.T) {
	s := NewServer("0")
	if _, err := s.NewI18n("en", []string{"en", "ru", "pl"}); err != nil {
		t.Fatalf("NewI18n: %v", err)
	}
	if err := s.EnableWeb(); err != nil {
		t.Fatalf("EnableWeb: %v", err)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/ru/i18n", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Переключайте язык без полной перезагрузки") {
		t.Fatalf("localized i18n page: status=%d body=%q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "<html lang=ru") {
		t.Fatalf("localized document language: %q", response.Body.String())
	}
	stream := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/ru/i18n", nil)
	request.Header.Set("X-Runtime", "1")
	request.Header.Set("Accept", "application/x-ndjson")
	request.Header.Set("X-GOSH-Runtime", "navigate")
	s.router.ServeHTTP(stream, request)
	if vary := stream.Header().Get("Vary"); !strings.Contains(vary, "X-GOSH-Runtime") {
		t.Fatalf("runtime response must vary by representation headers: %q", vary)
	}
	if !strings.Contains(stream.Body.String(), `"lang":"ru"`) {
		t.Fatalf("runtime language frame: %q", stream.Body.String())
	}
	if !strings.Contains(string(embeddedRuntimeJS), "document.documentElement.lang = frame.lang") {
		t.Fatal("runtime does not update html lang")
	}
	for _, expected := range []string{`hreflang=en href=/i18n`, `hreflang=ru href=/ru/i18n`, `hreflang=pl href=/pl/i18n`, `hreflang=x-default href=/i18n`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("missing hreflang link %q in %q", expected, response.Body.String())
		}
	}
	missing := httptest.NewRecorder()
	s.router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "http://example.test/missing-page", nil))
	if strings.Contains(missing.Body.String(), "hreflang=") {
		t.Fatal("404 response must not include hreflang links")
	}
}

func TestViewportCSSFeatures(t *testing.T) {
	if _, err := resolveNodeBinary(); err == nil {
		if _, err := os.Stat(filepath.Join(systemTailwindDir, "node_modules", "postcss", "package.json")); err == nil {
			css, processErr := processFinalCSS(".hero{min-height:100dvh}.h-screen{height:100vh}")
			if processErr != nil {
				t.Fatal(processErr)
			}
			for _, expected := range []string{"min-height:100vh;min-height:100dvh", ".h-screen{height:100vh;height:100dvh}"} {
				if !strings.Contains(css, expected) {
					t.Fatalf("viewport css missing %q in %q", expected, css)
				}
			}
		}
	}
	chunks := buildCSSChunks([]string{".one{color:red}", ".two{color:blue}"}, false, 1, 1)
	if len(chunks) != 1 || !strings.Contains(chunks[0], ".one") || !strings.Contains(chunks[0], ".two") {
		t.Fatalf("splitCss=false must produce one stylesheet: %#v", chunks)
	}
	chunks = buildCSSChunks([]string{
		".a{" + strings.Repeat("x", 25) + "}.b{" + strings.Repeat("y", 25) + "}.c{" + strings.Repeat("z", 15) + "}",
	}, true, 30, 60)
	if len(chunks) != 2 || len(chunks[0]) < 30 || len(chunks[1]) < 30 || len(chunks[0]) > 60 || len(chunks[1]) > 60 {
		t.Fatalf("split CSS must balance chunks within min/max limits: %#v", chunks)
	}
	for _, expected := range []string{"Runtime.prototype.scanReveal", "data-gosh-reveal-repeat", "IntersectionObserver", "requestAnimationFrame"} {
		if !strings.Contains(string(embeddedRuntimeJS), expected) {
			t.Fatalf("runtime reveal support is missing %q", expected)
		}
	}
}

func TestRevealSSRClassesPreventHydrationFlash(t *testing.T) {
	classes := revealSSRClasses([]Attribute{
		{Name: "data-gosh-reveal", Value: "slide-left"},
		{Name: "data-gosh-reveal-speed", Value: "slow"},
	})
	if classes != "reveal-active reveal-slide-left reveal-slow" {
		t.Fatalf("SSR reveal classes = %q", classes)
	}
	if got := appendHTMLClass("rounded p-4", classes); got != "rounded p-4 reveal-active reveal-slide-left reveal-slow" {
		t.Fatalf("merged SSR class = %q", got)
	}
}

func TestRenderDoesNotExposePropsInHTML(t *testing.T) {
	component := &Component{Name: "card", Template: []Node{&TextNode{Text: "ok"}}}
	renderer := NewRenderer(&Components{items: map[string]*Component{"card": component}})
	result, err := renderer.Render("card", Props{"secret": "not for HTML"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(result.HTML, "data-gosh-props") || strings.Contains(result.HTML, "not for HTML") {
		t.Fatalf("SSR props leaked into HTML: %q", result.HTML)
	}
	if strings.Contains(string(embeddedRuntimeJS), "atob(") || strings.Contains(string(embeddedRuntimeJS), "data-gosh-props") {
		t.Fatal("runtime must not decode SSR props from HTML")
	}
}

func TestStoreStateScriptUsesPlainJSON(t *testing.T) {
	script := storeStateScript(map[string]map[string]any{"cart": {"count": 2}})
	if !strings.Contains(script, `id="gosh-store-state"`) || !strings.Contains(script, `"count":2`) {
		t.Fatalf("store state script: %q", script)
	}
	if strings.Contains(script, "base64") || !strings.Contains(string(embeddedRuntimeJS), "initialStoreState") || !strings.Contains(string(embeddedRuntimeJS), `case "store-state":`) {
		t.Fatal("store hydration must use the JSON runtime protocol")
	}
}

func TestPlaygroundOverridesWebSourcesOnlyWhenEnabled(t *testing.T) {
	name := fmt.Sprintf("__goserver_overlay_%d.gosh", time.Now().UnixNano())
	overlay := filepath.Join(playgroundDir, "pages", name)
	if err := os.MkdirAll(filepath.Dir(overlay), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, []byte("<template>playground</template>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(overlay) })

	playgroundEnabled.Store(true)
	t.Cleanup(func() { playgroundEnabled.Store(false) })
	virtual := filepath.Join(systemPagesDir, name)
	data, err := sourceReadFile(virtual)
	if err != nil || string(data) != "<template>playground</template>" {
		t.Fatalf("overlay read: data=%q err=%v", data, err)
	}

	playgroundEnabled.Store(false)
	if _, err := sourceReadFile(virtual); !os.IsNotExist(err) {
		t.Fatalf("disabled overlay must be invisible, err=%v", err)
	}
}

func TestTenantWebOverlaysPagesAndContent(t *testing.T) {
	id := fmt.Sprintf("web-tenant-%d", time.Now().UnixNano())
	root := filepath.Join(tenantRootDir, id)
	if err := os.MkdirAll(filepath.Join(root, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "components", "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "teleport"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pages", "tenant-only.gosh"), []byte("<template><main>tenant page</main></template>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content", "tenant-only.md"), []byte("# Tenant post"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "ui", "CursorCreative.gosh"), []byte("<template><aside>tenant cursor</aside></template>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "teleport", "CursorCreative.client.gosh"), []byte("<template><UiCursorCreative /></template>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	app, err := NewWebApp()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://tenant.test/tenant-only", nil)
	request = request.WithContext(context.WithValue(request.Context(), tenantContextKey{}, &Tenant{ID: id}))
	page, _, status, err := app.selectTenantPage(request)
	if err != nil || status != http.StatusOK || page == nil || page.RelativePath != "tenant-only.gosh" {
		t.Fatalf("tenant page: page=%#v status=%d err=%v", page, status, err)
	}
	tenant, err := app.tenantWebFor(request)
	if err != nil || tenant == nil || tenant.content["tenant-only"].Title != "Tenant post" {
		t.Fatalf("tenant content: tenant=%#v err=%v", tenant, err)
	}
	component, ok := tenant.components.Get("UiCursorCreative")
	if !ok || !strings.Contains(component.Source, "tenant cursor") {
		t.Fatalf("tenant component override: %#v", component)
	}
	teleport, ok := tenant.teleports.Get("CursorCreative")
	if !ok || !strings.Contains(teleport.Path, filepath.Join(root, "teleport")) {
		t.Fatalf("tenant teleport override: %#v", teleport)
	}
}

func TestRuntimeStreamRequiresNDJSONFetch(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/i18n", nil)
	request.Header.Set("X-Runtime", "1")
	if isRuntimeRequest(request) {
		t.Fatal("a document request must not receive an NDJSON stream")
	}
	request.Header.Set("Accept", "application/x-ndjson")
	request.Header.Set("X-GOSH-Runtime", "navigate")
	if !isRuntimeRequest(request) {
		t.Fatal("runtime fetch with NDJSON accept must receive a stream")
	}
	request.Header.Set("Sec-Fetch-Dest", "document")
	if isRuntimeRequest(request) {
		t.Fatal("document navigation must not receive an NDJSON stream")
	}
}

func TestFullHTMLFallbackMovesNodesInsteadOfStringifyingNodeList(t *testing.T) {
	source := string(embeddedRuntimeJS)
	if !strings.Contains(source, "while (imported.firstChild) current.appendChild(imported.firstChild);") {
		t.Fatal("full HTML fallback must move parsed app nodes individually")
	}
	if strings.Contains(source, "replaceChildren(document.importNode(incoming, true).childNodes)") {
		t.Fatal("full HTML fallback still passes NodeList as one child")
	}
}

func TestAssetLinksRespectSplitCSS(t *testing.T) {
	newApp := func(split bool, minSize int) *App {
		return &App{
			clientURL: "/_gosh/entry/test.js",
			commonCSS: "/* common */ .common{display:block}",
			styles:    map[string]string{},
			config: logic.RuntimeConfig{Render: struct {
				DefaultLayout      string `json:"defaultLayout"`
				EarlyHints         bool   `json:"earlyHints"`
				ServerTiming       bool   `json:"serverTiming"`
				PreloadRuntime     bool   `json:"preloadRuntime"`
				PreloadPageStyles  bool   `json:"preloadPageStyles"`
				SplitCSS           bool   `json:"splitCss"`
				CSSMinChunkSize    int    `json:"cssMinChunkSize"`
				CSSMaxChunkSize    int    `json:"cssMaxChunkSize"`
				SPALoadingTemplate bool   `json:"spaLoadingTemplate"`
			}{SplitCSS: split, CSSMinChunkSize: minSize, CSSMaxChunkSize: 64}},
		}
	}
	result := RenderResult{CSSParts: []string{".page{color:red}"}}
	if _, urls := newApp(false, 1).assetLinks(nil, result); len(urls) != 1 {
		t.Fatalf("splitCss=false must emit one stylesheet, got %d", len(urls))
	}
	splitApp := newApp(true, 1024)
	_, urls := splitApp.assetLinks(nil, result)
	if len(urls) != 2 {
		t.Fatalf("splitCss=true must emit common and page stylesheets, got %d", len(urls))
	}
	if strings.Contains(splitApp.styles[strings.TrimSuffix(strings.TrimPrefix(urls[0], "/_gosh/style/"), ".css")], ".page") {
		t.Fatalf("common stylesheet must not contain page CSS: %q", splitApp.styles[strings.TrimSuffix(strings.TrimPrefix(urls[0], "/_gosh/style/"), ".css")])
	}
	large := RenderResult{CSSParts: []string{strings.Repeat(".page{color:red}", 20)}}
	if _, urls := newApp(true, 32).assetLinks(nil, large); len(urls) < 2 {
		t.Fatalf("large page CSS must retain a shared common asset, got %d", len(urls))
	}
}

func TestAssetLinksIncludeLazyCSSWhenSplitDisabled(t *testing.T) {
	lazy := &Component{Name: "LazyPanel", Mode: ModeClient, Styles: []StyleBlock{{Content: ".lazy-panel{display:block}"}}}
	page := &Page{View: &Component{Template: []Node{&ElementNode{Tag: "LazyPanel", IsComponent: true}}}}
	app := &App{
		components: &Components{items: map[string]*Component{normalizeComponentLookup(lazy.Name): lazy}},
		layouts:    &Components{items: map[string]*Component{}},
		commonCSS:  ".common{display:block}",
		styles:     map[string]string{},
		config: logic.RuntimeConfig{Render: struct {
			DefaultLayout      string `json:"defaultLayout"`
			EarlyHints         bool   `json:"earlyHints"`
			ServerTiming       bool   `json:"serverTiming"`
			PreloadRuntime     bool   `json:"preloadRuntime"`
			PreloadPageStyles  bool   `json:"preloadPageStyles"`
			SplitCSS           bool   `json:"splitCss"`
			CSSMinChunkSize    int    `json:"cssMinChunkSize"`
			CSSMaxChunkSize    int    `json:"cssMaxChunkSize"`
			SPALoadingTemplate bool   `json:"spaLoadingTemplate"`
		}{SplitCSS: false}},
	}
	_, urls := app.assetLinks(page, RenderResult{CSSParts: []string{".page{display:grid}"}})
	if len(urls) != 1 {
		t.Fatalf("splitCss=false must use one stylesheet, got %d", len(urls))
	}
	id := strings.TrimSuffix(strings.TrimPrefix(urls[0], "/_gosh/style/"), ".css")
	css := app.styles[id]
	for _, expected := range []string{".common", ".page", ".lazy-panel"} {
		if !strings.Contains(css, expected) {
			t.Fatalf("unified CSS is missing %q: %q", expected, css)
		}
	}
}

func TestSplitCSSDisabledUsesOneApplicationWideStylesheet(t *testing.T) {
	app := &App{
		commonCSS:  ".common{display:block}",
		unifiedCSS: ".common{display:block}.home{color:blue}.user{color:green}",
		styles:     map[string]string{},
		config: logic.RuntimeConfig{Render: struct {
			DefaultLayout      string `json:"defaultLayout"`
			EarlyHints         bool   `json:"earlyHints"`
			ServerTiming       bool   `json:"serverTiming"`
			PreloadRuntime     bool   `json:"preloadRuntime"`
			PreloadPageStyles  bool   `json:"preloadPageStyles"`
			SplitCSS           bool   `json:"splitCss"`
			CSSMinChunkSize    int    `json:"cssMinChunkSize"`
			CSSMaxChunkSize    int    `json:"cssMaxChunkSize"`
			SPALoadingTemplate bool   `json:"spaLoadingTemplate"`
		}{SplitCSS: false}},
	}
	_, home := app.assetLinks(nil, RenderResult{CSSParts: []string{".home{color:blue}"}})
	_, user := app.assetLinks(nil, RenderResult{CSSParts: []string{".user{color:green}"}})
	if len(home) != 1 || len(user) != 1 || home[0] != user[0] {
		t.Fatalf("splitCss=false must reuse one stylesheet: home=%#v user=%#v", home, user)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(home[0], "/_gosh/style/"), ".css")
	for _, expected := range []string{".common", ".home", ".user"} {
		if !strings.Contains(app.styles[id], expected) {
			t.Fatalf("unified stylesheet is missing %q: %q", expected, app.styles[id])
		}
	}
}
