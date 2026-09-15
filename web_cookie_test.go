package goserver

import (
	"bytes"
	"os"
	"strings"
	"testing"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestComponentStoreHelperUsesRuntimeStoreRegistry(t *testing.T) {
	if !bytes.Contains(embeddedRuntimeJS, []byte("Runtime.prototype.getStore")) {
		t.Fatal("component useStore/watchStore helpers have no Runtime.getStore implementation")
	}
}

func TestFilterCookieStoreOmitsCookieCodeWhenDisabled(t *testing.T) {
	stores := []clientSource{{name: "cart", path: "cart.js"}, {name: "cookies", path: "cookies.js"}, {name: "preferences", path: "preferences.js"}}
	got := filterCookieStore(append([]clientSource(nil), stores...), false)
	if len(got) != 2 || got[0].name != "cart" || got[1].name != "preferences" {
		t.Fatalf("disabled cookie control retained its client store: %#v", got)
	}
	enabled := filterCookieStore(append([]clientSource(nil), stores...), true)
	if len(enabled) != len(stores) {
		t.Fatalf("enabled cookie control lost its client store: %#v", enabled)
	}
}

func TestDisabledCookieControlOmitsGlobalComponentRouteCode(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatal(err)
	}
	layouts, err := LoadComponents(systemLayoutsDir)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		t.Fatal(err)
	}
	page, ok := pages.ByRelative("index.gosh")
	if !ok {
		t.Fatal("index page is absent")
	}
	sources := routeClientSources(page, components, layouts, "default", map[string]bool{}, map[string]int{}, false)
	joined := ""
	for _, source := range sources {
		joined += source
	}
	for _, forbidden := range []string{"[data-cookie-banner]", "[data-cookie-settings-overlay]"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("disabled cookie UI leaked %q into the route client chunk", forbidden)
		}
	}
}

func TestCookieComponentsRenderAndKeepEmbedsInert(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatal(err)
	}
	layouts, err := LoadComponents(systemLayoutsDir)
	if err != nil {
		t.Fatal(err)
	}
	renderer := NewRenderer(components)
	ctx := &logic.Context{Path: "/"}
	page := RenderResult{HTML: "<main>content</main>", Data: Props{"siteName": "test"}}
	rendered, err := renderer.RenderLayout(layouts.Must("default"), page, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"data-cookie-banner", "data-cookie-settings-overlay", "We use cookies"} {
		if !strings.Contains(rendered.HTML, marker) {
			t.Fatalf("default layout is missing %q: %s", marker, rendered.HTML)
		}
	}

	youtube, err := renderer.RenderComponentContext(components.Must("ConsentYoutube"), Props{"videoId": "dQw4w9WgXcQ"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	template := strings.Index(youtube.HTML, "<script type=\"text/plain\"")
	iframe := strings.Index(youtube.HTML, "<iframe")
	closeTemplate := strings.Index(youtube.HTML, "</script>")
	if template < 0 || iframe < template || closeTemplate < iframe {
		t.Fatalf("YouTube iframe must remain inert inside consent source before consent: %s", youtube.HTML)
	}
	if strings.Contains(youtube.HTML[:template], "<iframe") {
		t.Fatalf("live YouTube iframe rendered before consent: %s", youtube.HTML)
	}
}

func TestCookieSettingsCloseRestoresPendingBanner(t *testing.T) {
	source, err := os.ReadFile("web/stores/cookies.js")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	for _, required := range []string{
		"draftPreferences",
		"openSettings() {",
		"state.preferences.status === \"pending\"",
		"requestConsent(category)",
		"draftPreferences: draft(get().preferences, requestedCategory)",
	} {
		if !strings.Contains(code, required) {
			t.Fatalf("cookie store does not preserve pending consent flow: missing %q", required)
		}
	}
}

func TestConsentRequestPreselectsItsCategory(t *testing.T) {
	storeSource, err := os.ReadFile("web/stores/cookies.js")
	if err != nil {
		t.Fatal(err)
	}
	wrapperSource, err := os.ReadFile("web/components/cookie/ConsentWrapper.gosh")
	if err != nil {
		t.Fatal(err)
	}
	modalSource, err := os.ReadFile("web/components/cookie/SettingsModal.gosh")
	if err != nil {
		t.Fatal(err)
	}
	for file, required := range map[string][]string{
		"store":   {"requestConsent(category)", "categories.includes(category)", "draftPreferences: draft(get().preferences, requestedCategory)"},
		"wrapper": {"requestConsent(root.dataset.category)"},
		"modal":   {"state.draftPreferences[category] === true", "setDraftPreference(section.dataset.cookieCategory, event.detail.checked)", "savePreferences()"},
	} {
		var code string
		switch file {
		case "store":
			code = string(storeSource)
		case "wrapper":
			code = string(wrapperSource)
		case "modal":
			code = string(modalSource)
		}
		for _, marker := range required {
			if !strings.Contains(code, marker) {
				t.Fatalf("%s does not preselect requested consent category: missing %q", file, marker)
			}
		}
	}
}

func TestCookieBannerPersistsAcrossClientNavigation(t *testing.T) {
	source, err := os.ReadFile("web/components/cookie/Banner.gosh")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	for _, required := range []string{
		"data-cookie-banner-global",
		"document.body.append(root)",
		"if (persisted && incoming !== persisted) incoming.remove()",
	} {
		if !strings.Contains(code, required) {
			t.Fatalf("cookie banner is not retained across navigation: missing %q", required)
		}
	}
}
