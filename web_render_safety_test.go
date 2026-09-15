package goserver

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFinalScannerPreservesContent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<script>// comment\nrun();\n</script>", "<script nonce=\"abc\">run()</script>"},
		{"<pre> a\n\tb </pre><textarea> x\n y</textarea>", "<pre> a&#10;\tb </pre><textarea> x&#10; y</textarea>"},
		{`<!-- <script>fake</script> --><span>a</span> <span>b</span>`, `<!-- <script>fake</script> --><span>a</span> <span>b</span>`},
		{`<SCRIPT data-note="nonce=x">a < b</SCRIPT>`, `<SCRIPT nonce="abc" data-note="nonce=x">a < b</SCRIPT>`},
		{`<script NONCE = 'existing'>x</script>`, `<script NONCE = 'existing'>x</script>`},
		{"<script>let x='</scripture>';\nrun()</script>", "<script nonce=\"abc\">let x=\"</scripture>\";run()</script>"},
	}
	for _, c := range cases {
		if got := string(finalizeTrustedHTML([]byte(c.in), "abc")); got != c.want {
			t.Errorf("input %q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestFinalScannerOneLineSyntax(t *testing.T) {
	source := "<div\n title=\"a\nb\"><script>const x = `a\nb`;\nconsole.log(x);</script><style>p {\n color:red;\n}</style><script type=\"application/json\">{\n\"x\":1\n}</script></div>"
	got := string(finalizeTrustedHTML([]byte(source), "abc"))
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("response has line breaks: %q", got)
	}
	if !strings.Contains(got, "<div  title=") && !strings.Contains(got, "<div title=") {
		t.Fatalf("tag whitespace broken: %s", got)
	}
	if !strings.Contains(got, `{"x":1}`) {
		t.Fatalf("JSON changed: %s", got)
	}
}

func TestDocumentFormattingDoesNotBecomeEntities(t *testing.T) {
	source := "\r\n<!DOCTYPE html>\r\n<html>\r\n<head>\r\n\t<meta charset=\"utf-8\">\r\n</head><body><span>one</span>\r\n<span>two</span></body></html>"
	got := string(finalizeTrustedHTML([]byte(source), "nonce"))
	if !strings.HasPrefix(got, "<!DOCTYPE html>") || strings.ContainsAny(got, "\r\n") || strings.Contains(got, "&#10;") || strings.Contains(got, "&#13;") {
		t.Fatalf("formatting leaked into response: %q", got)
	}
	if !strings.Contains(got, "</span> <span>") {
		t.Fatal("inline word separator lost")
	}
}

func TestShowcaseDocumentSingleLine(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		t.Fatal(err)
	}
	page, ok := pages.ByRelative("test.gosh")
	if !ok {
		t.Fatal("missing test page")
	}
	result, err := NewRenderer(components).RenderComponent(page.View, Props{})
	if err != nil {
		t.Fatal(err)
	}
	base, err := loadBaseTemplate()
	if err != nil {
		t.Fatal(err)
	}
	app := &App{base: base}
	result.RuntimePlan = ModulePlan{Bindings: []ModuleBinding{{Src: "/test.js", Target: "#app"}}}
	result.RuntimeURL = "/runtime.js"
	body, err := app.renderBase(result, page, 200, "nonce", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny(body, "\r\n") {
		t.Fatal("showcase document has physical line breaks")
	}
}

func TestStaticRenderPreservesDynamicAndScopes(t *testing.T) {
	source := `<div><section class="x"><span>hello</span> <b>world</b></section><p m-if="show">{{ value }}</p><p m-else>other</p></div>`
	parse := func(prepared bool) []Node {
		nodes, err := newTemplateParser(source).Parse()
		if err != nil {
			t.Fatal(err)
		}
		applyScope(nodes, "data-v-test")
		if prepared {
			prepareTemplateMetadata(nodes)
		}
		return nodes
	}
	plain, compiled := parse(false), parse(true)
	for _, show := range []bool{true, false} {
		scope := NewScope(nil)
		scope.Set("show", show)
		scope.Set("value", `<&"`)
		render := func(nodes []Node) string {
			var out strings.Builder
			s := &renderState{}
			if err := s.renderNodes(&out, nodes, renderContext{scope: scope, root: true, parentRootScopes: []string{"data-v-parent"}}); err != nil {
				t.Fatal(err)
			}
			return out.String()
		}
		if a, b := render(plain), render(compiled); a != b {
			t.Fatalf("compiled changed HTML: %s != %s", a, b)
		}
	}
	if compiled[0].(*ElementNode).Children[0].(*ElementNode).staticFragment == "" {
		t.Fatal("static subtree not compiled")
	}
}

func TestPagePlanMetadataBounded(t *testing.T) {
	app := &App{}
	for i := 0; i < runtimeStateLimit+10; i++ {
		app.rememberPagePlan(string(rune(i+1)), ModulePlan{})
	}
	if len(app.pagePlans) > runtimeStateLimit || len(app.pagePlanOrder) > runtimeStateLimit {
		t.Fatal("unbounded metadata")
	}
	token := string(rune(runtimeStateLimit + 10))
	app.pagePlanExpiry[token] = time.Now().Add(-time.Second)
	if _, ok := app.lookupPagePlan(token); ok {
		t.Fatal("expired plan returned")
	}
}

func TestWebRendererUsesServerLogLevel(t *testing.T) {
	var output bytes.Buffer
	r := &Renderer{logger: log.New(levelWriter{Target: &output, Minimum: LogOff}, "", 0)}
	r.logError("render failed")
	if output.Len() != 0 {
		t.Fatal("off ignored")
	}
	r.logger = log.New(levelWriter{Target: &output, Minimum: LogError}, "", 0)
	r.logError("render failed")
	if !strings.Contains(output.String(), "ERROR: render failed") {
		t.Fatal("error suppressed")
	}
}

func TestDynamicURLRejectsExecutableSchemes(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "java\nscript:alert(1)", "data:text/html,<script>x</script>"} {
		var out strings.Builder
		if err := writeDynamicHTMLAttribute(&out, "href", value); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "#ZgotmplZ") {
			t.Fatalf("unsafe URL accepted: %q", value)
		}
	}
	for _, value := range []string{"/page", "https://example.test/a", "#tab", "mailto:a@example.test"} {
		if !safeDynamicURL(value) {
			t.Fatalf("valid URL rejected: %q", value)
		}
	}
}

func TestRawTextInterpolationUsesContextEscaping(t *testing.T) {
	nodes, err := newTemplateParser(`<script>const s = "{{ payload }}";</script><style>.x{color:{{ color }}}</style>`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	prepareTemplateMetadata(nodes)
	scope := NewScope(nil)
	scope.Set("payload", `";</script><script>alert(1)</script>`)
	scope.Set("color", "red")
	var out strings.Builder
	state := &renderState{}
	if err := state.renderNodes(&out, nodes, renderContext{scope: scope}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "<script>") != 1 || !strings.Contains(out.String(), "color:red") || strings.Contains(out.String(), "<script>alert") {
		t.Fatalf("unsafe or broken output: %s", out.String())
	}
}

func TestRuntimeReadsKeepIsolationAfterUnlock(t *testing.T) {
	store := newRuntimeStateStore()
	token, err := store.Put("owner", Props{"nested": map[string]any{"value": "original"}})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			props, ok := store.Get("owner", token)
			if !ok {
				t.Error("missing state")
				return
			}
			nested := props["nested"].(map[string]any)
			if nested["value"] != "original" {
				t.Error("mutation leaked")
			}
			nested["value"] = "changed"
		}()
	}
	group.Wait()
	if _, ok := store.Get("other", token); ok {
		t.Fatal("owner isolation lost")
	}
}
