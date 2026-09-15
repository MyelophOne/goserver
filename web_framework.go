package goserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	stdtemplate "html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	_ "github.com/myelophone/goserver/web/modules"
	logic "github.com/myelophone/goserver/web/runtime"
	_ "github.com/myelophone/goserver/web/system/generated"
	"golang.org/x/sync/singleflight"
)

type Components struct {
	root  string
	items map[string]*Component
	names []string
}

func (c *Components) Get(name string) (*Component, bool) {
	component, ok := c.items[normalizeComponentLookup(name)]
	return component, ok
}

func (c *Components) Must(name string) *Component {
	component, ok := c.Get(name)
	if !ok {
		panic(fmt.Sprintf("component %q not found", name))
	}
	return component
}

func (c *Components) Has(name string) bool {
	_, ok := c.Get(name)
	return ok
}

func (c *Components) Names() []string {
	out := make([]string, len(c.names))
	copy(out, c.names)
	return out
}

func (c *Components) Len() int { return len(c.names) }

type ServerRef struct {
	Path    string
	Export  string
	Handler logic.Handler
}

type ComponentMode string

const (
	ModeUniversal ComponentMode = "universal"
	ModeServer    ComponentMode = "server"
	ModeClient    ComponentMode = "client"
)

var staticScriptElementPattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
var inlineStyleAttributePattern = regexp.MustCompile(`(?is)\sstyle\s*=\s*"([^"]*)"`)
var cssDeclarationNamePattern = regexp.MustCompile(`^(?:--[a-zA-Z0-9_-]+|-?[a-zA-Z][a-zA-Z0-9-]*)$`)

func compactHTMLIntertagWhitespace(document []byte) []byte {
	if !bytes.ContainsAny(document, "\r\n\t") {
		return document
	}
	out := make([]byte, 0, len(document))
	rawTag := ""
	for i := 0; i < len(document); {
		if rawTag != "" && document[i] == '<' {
			closePrefix := []byte("</" + rawTag)
			if len(document)-i < len(closePrefix) || !bytes.EqualFold(document[i:i+len(closePrefix)], closePrefix) {
				out = append(out, document[i])
				i++
				continue
			}
		}
		if document[i] != '<' {
			if rawTag != "" && (document[i] == '\r' || document[i] == '\n' || document[i] == '\t') {
				if len(out) > 0 && out[len(out)-1] != ' ' {
					out = append(out, ' ')
				}
				i++
				continue
			}
			if rawTag == "" && (document[i] == '\r' || document[i] == '\n' || document[i] == '\t') {
				if len(out) > 0 && out[len(out)-1] != ' ' {
					out = append(out, ' ')
				}
				i++
				continue
			}
			out = append(out, document[i])
			i++
			continue
		}
		end := htmlTagEnd(document, i)
		if end < 0 {
			out = append(out, document[i:]...)
			break
		}
		tag, closing := htmlTagName(document[i+1 : end])
		out = append(out, document[i:end+1]...)
		i = end + 1
		if closing && tag == rawTag {
			rawTag = ""
		} else if !closing && (tag == "pre" || tag == "script" || tag == "style" || tag == "textarea") {
			rawTag = tag
		}
		if rawTag != "" {
			continue
		}
		spaceEnd := i
		for spaceEnd < len(document) && (document[spaceEnd] == ' ' || document[spaceEnd] == '\t' || document[spaceEnd] == '\r' || document[spaceEnd] == '\n') {
			spaceEnd++
		}
		if spaceEnd > i && spaceEnd < len(document) && document[spaceEnd] == '<' {
			i = spaceEnd
		}
	}
	return out
}

func hasHTMLNonceAttribute(tag []byte) bool {
	for i := 0; i+5 < len(tag); i++ {
		if !equalFoldASCIIWord(tag[i:i+5], "nonce") {
			continue
		}
		if i > 0 && isHTMLNameChar(tag[i-1]) {
			continue
		}
		j := i + 5
		for j < len(tag) && isHTMLSpace(tag[j]) {
			j++
		}
		if j < len(tag) && tag[j] == '=' {
			return true
		}
	}
	return false
}

func htmlTagEnd(document []byte, start int) int {
	var quote byte
	for i := start + 1; i < len(document); i++ {
		current := document[i]
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
		} else if current == '>' {
			return i
		}
	}
	return -1
}

func htmlTagName(source []byte) (name string, closing bool) {
	source = bytes.TrimSpace(source)
	if len(source) == 0 || source[0] == '!' || source[0] == '?' {
		return "", false
	}
	if source[0] == '/' {
		closing = true
		source = bytes.TrimSpace(source[1:])
	}
	for i, current := range source {
		if current == ' ' || current == '\t' || current == '\r' || current == '\n' || current == '/' {
			source = source[:i]
			break
		}
	}
	return strings.ToLower(string(source)), closing
}

func nonceStylesForHTML(markup string) (string, string) {
	if !hasInlineStyleAttribute(markup) {
		return markup, ""
	}
	rules := map[string]string{}
	markup = inlineStyleAttributePattern.ReplaceAllStringFunc(markup, func(match string) string {
		parts := inlineStyleAttributePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return ""
		}
		declarations, ok := safeStyleDeclarations(html.UnescapeString(parts[1]))
		if !ok || declarations == "" {
			return ""
		}
		id := "s" + hashText(declarations)[:12]
		rules[id] = declarations
		return ` data-gosh-style="` + id + `"`
	})
	if len(rules) == 0 {
		return markup, ""
	}
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var css strings.Builder
	for _, id := range ids {
		css.WriteString(`[data-gosh-style="` + id + `"]{` + rules[id] + `}`)
	}
	return markup, minifyCSS(css.String())
}

func hasInlineStyleAttribute(markup string) bool {
	for i := 0; i+5 < len(markup); i++ {
		if !equalFoldASCIIWordString(markup[i:i+5], "style") {
			continue
		}
		if i == 0 || !isHTMLSpace(markup[i-1]) {
			continue
		}
		j := i + 5
		for j < len(markup) && isHTMLSpace(markup[j]) {
			j++
		}
		if j < len(markup) && markup[j] == '=' {
			return true
		}
	}
	return false
}

func equalFoldASCIIWord(value []byte, word string) bool {
	if len(value) != len(word) {
		return false
	}
	for i := range value {
		current := value[i]
		if current >= 'A' && current <= 'Z' {
			current += 'a' - 'A'
		}
		if current != word[i] {
			return false
		}
	}
	return true
}

func equalFoldASCIIWordString(value, word string) bool {
	if len(value) != len(word) {
		return false
	}
	for i := range value {
		current := value[i]
		if current >= 'A' && current <= 'Z' {
			current += 'a' - 'A'
		}
		if current != word[i] {
			return false
		}
	}
	return true
}

func isHTMLSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func isHTMLNameChar(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-' || value == '_' || value == ':'
}

func safeStyleDeclarations(value string) (string, bool) {
	declarations := make([]string, 0, 4)
	for _, declaration := range strings.Split(value, ";") {
		name, rawValue, found := strings.Cut(declaration, ":")
		name, rawValue = strings.TrimSpace(name), strings.TrimSpace(rawValue)
		if name == "" && rawValue == "" {
			continue
		}
		if !found || !cssDeclarationNamePattern.MatchString(name) || rawValue == "" {
			return "", false
		}
		lower := strings.ToLower(rawValue)
		if strings.ContainsAny(rawValue, "{}<>") || strings.Contains(lower, "expression(") || strings.Contains(lower, "javascript:") || strings.Contains(lower, "-moz-binding") || strings.Contains(lower, "behavior:") {
			return "", false
		}
		declarations = append(declarations, name+":"+rawValue)
	}
	return strings.Join(declarations, ";"), true
}

var embeddedRuntimeJS []byte

var embeddedVitalsJS []byte

var embeddedWebSocketJS []byte

var playgroundEnabled atomic.Bool

type Component struct {
	Name         string
	Path         string
	RelativePath string
	Source       string
	Mode         ComponentMode
	ServerRefs   []ServerRef
	Layout       string
	Head         string

	TemplateSource string
	Template       []Node

	ScriptSetup *Block
	Scripts     []Block
	Styles      []StyleBlock

	PreparedSetupSource string
	HasRuntimeAction    bool

	ScopeID string

	cssOnce sync.Once
	css     string
}

func (c *Component) GlobalCSS() string {
	var b strings.Builder
	for _, style := range c.Styles {
		if style.Scoped {
			continue
		}
		if strings.TrimSpace(style.Content) == "" {
			continue
		}
		b.WriteString(style.Content)
		if !strings.HasSuffix(style.Content, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (c *Component) ScopedCSS() string {
	if c.ScopeID == "" {
		return ""
	}

	var b strings.Builder
	for _, style := range c.Styles {
		if !style.Scoped || strings.TrimSpace(style.Content) == "" {
			continue
		}
		b.WriteString(scopeCSS(style.Content, c.ScopeID))
		if !strings.HasSuffix(style.Content, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (c *Component) CSS() string {
	if c == nil {
		return ""
	}
	c.cssOnce.Do(func() {
		c.css = c.GlobalCSS() + c.ScopedCSS()
	})
	return c.css
}

type Block struct {
	Content string
	Attrs   map[string]string
}

type StyleBlock struct {
	Content string
	Attrs   map[string]string
	Scoped  bool
	Module  bool
	Lang    string
}

const (
	systemBaseCSS              = ".gosh-component-root{display:contents}\n"
	frameworkVersion           = "0.16"
	systemComponentsDir        = "./web/components"
	systemPagesDir             = "./web/pages"
	systemLogicDir             = "./web/logic"
	systemLayoutsDir           = "./web/layouts"
	systemTeleportDir          = "./web/teleport"
	systemFrameworkTeleportDir = "./web/system/teleport"
	systemGlobalDir            = "./web/global"
	systemRuntimeDir           = "./web/system/runtime"
	systemTemplatesDir         = "./web/system/templates"
	systemPublicDir            = "./assets"
	systemTailwindDir          = "./web/system/tailwind"
	systemStoresDir            = "./web/stores"
	systemClientPluginsDir     = "./web/plugins"
	systemClientDir            = "./web/system/client"
	systemContentDir           = "./web/content"
	systemWebCSSDir            = "./web/css"
	systemFrameworkCSSDir      = "./web/system/css"
	systemTailwindGlobal       = "./web/system/tailwind/global.css"
	playgroundDir              = "./web/playground"
	tenantRootDir              = "./web/tenants"
)

type Node interface {
	node()
}

type TextNode struct {
	Text       string
	Prepared   bool
	Whitespace bool
}

func (*TextNode) node() {}

type InterpolationNode struct {
	Expression string
}

func (*InterpolationNode) node() {}

type CommentNode struct {
	Text string
}

func (*CommentNode) node() {}

type ElementNode struct {
	staticFragment     string
	contextTemplate    *stdtemplate.Template
	contextExpressions []string
	Tag                string
	IsComponent        bool
	SelfClosing        bool
	Attrs              []Attribute
	Children           []Node

	ScopeAttrs []string

	Prepared      bool
	HasIf         bool
	IfValue       string
	HasElseIf     bool
	ElseIfValue   string
	HasElse       bool
	IsSlot        bool
	IsClientOnly  bool
	RevealClasses string
}

func (*ElementNode) node() {}

type Attribute struct {
	Name string

	Value string

	Dynamic bool

	Event bool

	Directive bool

	Boolean bool

	Prepared      bool
	RenderedName  string
	ComponentProp string
	EvalValue     string
	SkipRender    bool
	StaticHTML    string
}

func prepareTemplateMetadata(nodes []Node) {
	for _, node := range nodes {
		if text, ok := node.(*TextNode); ok {
			text.Prepared = true
			text.Whitespace = strings.TrimSpace(text.Text) == ""
			continue
		}
		element, ok := node.(*ElementNode)
		if !ok {
			continue
		}
		element.Prepared = true
		element.IsSlot = strings.EqualFold(element.Tag, "slot")
		element.IsClientOnly = strings.EqualFold(element.Tag, "clientonly")
		element.RevealClasses = revealSSRClasses(element.Attrs)
		if attr, exists := findDirective(element.Attrs, "m-if"); exists {
			element.HasIf, element.IfValue = true, attributeEvalValue(attr)
		}
		if attr, exists := findDirective(element.Attrs, "m-else-if"); exists {
			element.HasElseIf, element.ElseIfValue = true, attributeEvalValue(attr)
		}
		_, element.HasElse = findDirective(element.Attrs, "m-else")
		for i := range element.Attrs {
			attr := &element.Attrs[i]
			attr.Prepared = true
			attr.RenderedName = renderedAttributeName(attr.Name)
			attr.ComponentProp = componentPropName(attr.Name)
			attr.EvalValue = strings.TrimSpace(attr.Value)
			attr.SkipRender = shouldSkipRenderedAttribute(*attr)
			if !attr.Dynamic && !attr.Event && !attr.SkipRender && attr.RenderedName != "" {
				attr.StaticHTML = staticHTMLAttribute(attr.RenderedName, *attr)
			}
		}
		prepareTemplateMetadata(element.Children)
		prepareRawTextExpressions(element)
		if staticRenderTree(element) {
			var output strings.Builder
			state := &renderState{}
			if err := state.renderElement(&output, element, renderContext{}); err == nil {
				element.staticFragment = output.String()
			}
		}
	}
}

func attributeEvalValue(attr Attribute) string {
	if attr.Prepared {
		return attr.EvalValue
	}
	return strings.TrimSpace(attr.Value)
}

func attributeRenderedName(attr Attribute) string {
	if attr.Prepared {
		return attr.RenderedName
	}
	return renderedAttributeName(attr.Name)
}

func attributeComponentProp(attr Attribute) string {
	if attr.Prepared {
		return attr.ComponentProp
	}
	return componentPropName(attr.Name)
}

type Props map[string]any

type ScriptBinding struct {
	Source string `json:"-"`
	Target string `json:"target"`
	Owner  string `json:"owner,omitempty"`
}

type ModuleBinding struct {
	Target   string `json:"target,omitempty"`
	Selector string `json:"selector,omitempty"`
	Src      string `json:"src,omitempty"`
	ID       string `json:"id,omitempty"`
	Chunk    string `json:"chunk,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

type ModulePlan struct {
	Bindings []ModuleBinding `json:"bindings,omitempty"`
}

type RenderResult struct {
	SEO           logic.SEOState
	HTML          string
	TeleportHTML  string
	CSS           string
	RuntimeURL    string   `json:"runtimeURL,omitempty"`
	StyleURLs     []string `json:"styleURLs,omitempty"`
	CSSParts      []string
	Head          string
	Scripts       []ScriptBinding
	Data          Props
	Stores        map[string]map[string]any
	CacheTags     []string
	RuntimeStates map[string]Props
	RuntimePlan   ModulePlan `json:"-"`

	Components []string
}

type Renderer struct {
	logger     *log.Logger
	components *Components
	loader     string
}

const defaultLoaderHTML = `<div class="gosh-system-loader" role="status" aria-label="Loading"><svg width="26" height="26" viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg" class="loadersvg" aria-hidden="true"><g fill="none" fill-rule="evenodd"><g fill="var(--loader-fill-color, #333)"><path d="M16 32a16 16 0 110-32 16 16 0 010 32zm0-2a14 14 0 100-28 14 14 0 000 28z" fill-rule="nonzero" opacity=".1"/><path d="M16 2V0A16 16 0 000 16h2A14 14 0 0116 2z"/></g></g></svg></div>`

func NewRenderer(components *Components, loader ...string) *Renderer {
	r := &Renderer{components: components, loader: defaultLoaderHTML}
	if len(loader) > 0 && strings.TrimSpace(loader[0]) != "" {
		r.loader = loader[0]
	}
	return r
}

func (r *Renderer) Render(name string, props Props) (RenderResult, error) {
	component, ok := r.components.Get(name)
	if !ok {
		return RenderResult{}, fmt.Errorf("component %q not found", name)
	}

	return r.RenderComponent(component, props)
}

func (r *Renderer) RenderComponent(component *Component, props Props) (RenderResult, error) {
	return r.RenderComponentContext(component, props, nil)
}

func (r *Renderer) RenderComponentContext(component *Component, props Props, logicCtx *logic.Context) (RenderResult, error) {
	return r.RenderComponentContextTarget(component, props, logicCtx, "#app")
}

func (r *Renderer) RenderComponentContextTarget(component *Component, props Props, logicCtx *logic.Context, mountTarget string) (RenderResult, error) {
	state := &renderState{
		renderer:      r,
		used:          make(map[string]*Component),
		logicCtx:      logicCtx,
		instanceSalt:  strconv.FormatUint(atomic.AddUint64(&renderSequence, 1), 36),
		runtimeStates: make(map[string]Props),
	}

	scope := NewScope(nil)
	for key, value := range props {
		scope.Set(key, value)
	}

	var out strings.Builder
	if component != nil {
		out.Grow(len(component.TemplateSource) * 2)
	}
	if err := state.renderComponent(&out, component, scope, nil, nil, mountTarget); err != nil {
		return RenderResult{}, err
	}

	names := make([]string, 0, len(state.used))
	for componentName := range state.used {
		names = append(names, componentName)
	}
	sort.Strings(names)

	var css strings.Builder
	cssParts := make([]string, 0, len(names))
	for _, componentName := range names {
		c := state.used[componentName]
		content := c.CSS()
		if strings.TrimSpace(content) == "" {
			continue
		}
		cssParts = append(cssParts, content)
		css.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			css.WriteByte('\n')
		}
	}

	var head strings.Builder
	for _, name := range names {
		c := state.used[name]
		if strings.TrimSpace(c.Head) != "" {
			head.WriteString(c.Head)
			head.WriteByte('\n')
		}
	}

	return RenderResult{
		HTML:          out.String(),
		CSS:           css.String(),
		CSSParts:      cssParts,
		Head:          head.String(),
		Scripts:       state.scripts,
		Data:          scope.Snapshot(),
		Stores:        logic.StoreState(logicCtx),
		CacheTags:     logic.CacheTags(logicCtx),
		RuntimeStates: state.runtimeStates,
		Components:    names,
	}, nil
}

func (r *Renderer) RenderLayout(layout *Component, page RenderResult, logicCtx *logic.Context) (RenderResult, error) {
	state := &renderState{renderer: r, used: make(map[string]*Component), logicCtx: logicCtx, instanceSalt: strconv.FormatUint(atomic.AddUint64(&renderSequence, 1), 36), runtimeStates: make(map[string]Props)}
	scope := NewScope(nil)
	for k, v := range page.Data {
		scope.Set(k, v)
	}
	var out strings.Builder
	if layout != nil {
		out.Grow(len(page.HTML) + len(layout.TemplateSource)*2)
	}
	if err := applyServerRender(layout, logicCtx, scope); err != nil {
		r.logError("web render %s server logic: %v", layout.Name, err)
		scope.Set("renderError", err.Error())
	}
	state.used[layout.Name] = layout
	for _, block := range layout.Scripts {
		if strings.TrimSpace(block.Content) != "" {
			state.scripts = append(state.scripts, ScriptBinding{Source: block.Content, Target: "#app", Owner: layout.Name})
		}
	}
	if layout.PreparedSetupSource != "" {
		state.scripts = append(state.scripts, ScriptBinding{Source: layout.PreparedSetupSource, Target: "#app", Owner: layout.Name})
	}
	if err := state.renderNodes(&out, layout.Template, renderContext{scope: scope, slot: &SlotContent{RawHTML: page.HTML, Scope: scope, Owner: layout}, owner: layout, root: true}); err != nil {
		return RenderResult{}, err
	}

	already := map[string]bool{}
	for _, n := range page.Components {
		already[n] = true
	}
	components := append([]string{}, page.Components...)
	var css strings.Builder
	css.WriteString(page.CSS)
	cssParts := append([]string(nil), page.CSSParts...)
	var head strings.Builder
	head.WriteString(page.Head)
	names := make([]string, 0, len(state.used))
	for n := range state.used {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := state.used[n]
		if !already[n] {
			componentCSS := c.CSS()
			css.WriteString(componentCSS)
			if strings.TrimSpace(componentCSS) != "" {
				cssParts = append(cssParts, componentCSS)
			}
			components = append(components, n)
			already[n] = true
		}
		if strings.TrimSpace(c.Head) != "" {
			head.WriteString(c.Head)
			head.WriteByte('\n')
		}
	}
	scripts := page.Scripts
	if len(state.scripts) > 0 {
		scripts = make([]ScriptBinding, 0, len(page.Scripts)+len(state.scripts))
		scripts = append(scripts, page.Scripts...)
		scripts = append(scripts, state.scripts...)
	}
	sort.Strings(components)
	runtimeStates := page.RuntimeStates
	if len(state.runtimeStates) > 0 {
		runtimeStates = make(map[string]Props, len(page.RuntimeStates)+len(state.runtimeStates))
		for token, props := range page.RuntimeStates {
			runtimeStates[token] = props
		}
		for token, props := range state.runtimeStates {
			runtimeStates[token] = props
		}
	}
	return RenderResult{HTML: out.String(), CSS: css.String(), CSSParts: cssParts, Head: head.String(), Scripts: scripts, Data: scope.Snapshot(), Stores: logic.StoreState(logicCtx), CacheTags: logic.CacheTags(logicCtx), RuntimeStates: runtimeStates, Components: components}, nil
}

type Scope struct {
	parent *Scope
	values map[string]any
}

func NewScope(parent *Scope) *Scope {
	return NewScopeCapacity(parent, 0)
}

func NewScopeCapacity(parent *Scope, capacity int) *Scope {
	if capacity < 0 {
		capacity = 0
	}
	return &Scope{
		parent: parent,
		values: make(map[string]any, capacity),
	}
}

func (s *Scope) Set(name string, value any) {
	s.values[name] = value
}

func (s *Scope) Get(name string) (any, bool) {
	for current := s; current != nil; current = current.parent {
		value, ok := current.values[name]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func (s *Scope) Snapshot() Props {
	out := Props{}
	var chain []*Scope
	for cur := s; cur != nil; cur = cur.parent {
		chain = append(chain, cur)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for k, v := range chain[i].values {
			out[k] = v
		}
	}
	return out
}

func applyServerRender(component *Component, ctx *logic.Context, scope *Scope) error {
	if component == nil || len(component.ServerRefs) == 0 {
		return nil
	}
	snapshot := scope.Snapshot()
	if err := logic.CallHook(logic.HookServerComponentBefore, &logic.HookPayload{App: logic.UseApp(), Key: component.Name, Data: snapshot}); err != nil {
		return err
	}
	props := logic.Props(snapshot)
	defer func() {
		_ = logic.CallHook(logic.HookServerComponentAfter, &logic.HookPayload{App: logic.UseApp(), Key: component.Name, Data: props})
	}()
	cfg := logic.MustRuntimeConfig()
	rule := cfg.ComponentRules[component.Name]
	var bgCtx *logic.Context
	for _, ref := range component.ServerRefs {
		h := ref.Handler
		if h == nil {
			var ok bool
			h, ok = logic.Resolve(ref.Export)
			if !ok {
				return fmt.Errorf("server export %q from %s is not registered", ref.Export, ref.Path)
			}
		}
		propsCopy := make(logic.Props, len(props))
		for k, v := range props {
			propsCopy[k] = v
		}
		data, _, err := logic.CachedComponentData(component.Name, ref.Export, propsCopy, rule, func() (logic.Data, error) {
			return h.Render(ctx, propsCopy)
		}, func() (logic.Data, error) {
			if bgCtx == nil {
				bgCtx = logic.CloneContextForBackground(ctx)
			}
			return h.Render(bgCtx, propsCopy)
		})
		if err != nil {
			return err
		}
		for k, v := range data {
			scope.Set(k, v)
			props[k] = v
		}
	}
	return nil
}

type SlotContent struct {
	Nodes   []Node
	Scope   *Scope
	Owner   *Component
	RawHTML string
}

var renderSequence uint64

type renderState struct {
	renderer      *Renderer
	used          map[string]*Component
	logicCtx      *logic.Context
	forceClient   bool
	instanceSeq   uint64
	instanceSalt  string
	scripts       []ScriptBinding
	runtimeStates map[string]Props
}

func (s *renderState) renderComponent(
	out *strings.Builder,
	component *Component,
	scope *Scope,
	slot *SlotContent,
	parentRootScopes []string,
	mountTarget string,
) error {
	if err := applyServerRender(component, s.logicCtx, scope); err != nil {
		s.renderer.logError("web render %s server logic: %v", component.Name, err)
		scope.Set("renderError", err.Error())
	}
	s.used[component.Name] = component
	for _, block := range component.Scripts {
		if strings.TrimSpace(block.Content) == "" {
			continue
		}
		s.scripts = append(s.scripts, ScriptBinding{Source: block.Content, Target: mountTarget, Owner: component.Name})
	}
	if component.PreparedSetupSource != "" {
		s.scripts = append(s.scripts, ScriptBinding{Source: component.PreparedSetupSource, Target: mountTarget, Owner: component.Name})
	}

	return s.renderNodes(out, component.Template, renderContext{
		scope:            scope,
		slot:             slot,
		owner:            component,
		root:             true,
		parentRootScopes: parentRootScopes,
	})
}

type renderContext struct {
	scope *Scope
	slot  *SlotContent
	owner *Component

	root          bool
	forceLazy     bool
	lazyHydration string

	parentRootScopes []string
}

func textNodeWhitespace(node *TextNode) bool {
	if node == nil {
		return true
	}
	if node.Prepared {
		return node.Whitespace
	}
	return strings.TrimSpace(node.Text) == ""
}

func elementIfDirective(element *ElementNode) (string, bool) {
	if element != nil && element.Prepared {
		return element.IfValue, element.HasIf
	}
	if element == nil {
		return "", false
	}
	attr, ok := findDirective(element.Attrs, "m-if")
	return attributeEvalValue(attr), ok
}

func elementElseIfDirective(element *ElementNode) (string, bool) {
	if element != nil && element.Prepared {
		return element.ElseIfValue, element.HasElseIf
	}
	if element == nil {
		return "", false
	}
	attr, ok := findDirective(element.Attrs, "m-else-if")
	return attributeEvalValue(attr), ok
}

func elementElseDirective(element *ElementNode) bool {
	if element != nil && element.Prepared {
		return element.HasElse
	}
	if element == nil {
		return false
	}
	_, ok := findDirective(element.Attrs, "m-else")
	return ok
}

func elementIsSlot(element *ElementNode) bool {
	if element != nil && element.Prepared {
		return element.IsSlot
	}
	return element != nil && strings.EqualFold(element.Tag, "slot")
}

func elementIsClientOnly(element *ElementNode) bool {
	if element != nil && element.Prepared {
		return element.IsClientOnly
	}
	return element != nil && strings.EqualFold(element.Tag, "clientonly")
}

func elementRevealClasses(element *ElementNode) string {
	if element != nil && element.Prepared {
		return element.RevealClasses
	}
	if element == nil {
		return ""
	}
	return revealSSRClasses(element.Attrs)
}

func (s *renderState) renderNodes(out *strings.Builder, nodes []Node, ctx renderContext) error {
	if s.logicCtx != nil && s.logicCtx.Request != nil {
		if err := s.logicCtx.Request.Context().Err(); err != nil {
			return err
		}
	}
	chainActive := false
	chainMatched := false
	for _, node := range nodes {
		if t, ok := node.(*TextNode); ok && textNodeWhitespace(t) {
			if err := s.renderNode(out, node, ctx); err != nil {
				s.renderer.logError("web render node: %v", err)
			}
			continue
		}
		if el, ok := node.(*ElementNode); ok {
			if value, exists := elementIfDirective(el); exists {
				v, err := evalExpression(value, ctx.scope)
				if err != nil {
					s.renderer.logError("web render m-if %q: %v", value, err)
					v = false
				}
				chainActive = true
				chainMatched = truthy(v)
				if !chainMatched {
					continue
				}
			} else if value, exists := elementElseIfDirective(el); exists && chainActive {
				if chainMatched {
					continue
				}
				v, err := evalExpression(value, ctx.scope)
				if err != nil {
					s.renderer.logError("web render m-else-if %q: %v", value, err)
					v = false
				}
				chainMatched = truthy(v)
				if !chainMatched {
					continue
				}
			} else if elementElseDirective(el) && chainActive {
				if chainMatched {
					chainActive = false
					continue
				}
				chainMatched = true
				chainActive = false
			} else {
				chainActive = false
				chainMatched = false
			}
		} else {
			chainActive = false
			chainMatched = false
		}
		if err := s.renderNode(out, node, ctx); err != nil {
			s.renderer.logError("web render node: %v", err)
		}
	}
	return nil
}

func (s *renderState) renderNode(out *strings.Builder, node Node, ctx renderContext) error {
	switch n := node.(type) {
	case *TextNode:
		out.WriteString(n.Text)
		return nil

	case *InterpolationNode:
		value, err := evalExpression(n.Expression, ctx.scope)
		if err != nil {
			return fmt.Errorf("{{ %s }}: %w", n.Expression, err)
		}
		out.WriteString(html.EscapeString(stringify(value)))
		return nil

	case *CommentNode:
		out.WriteString("<!--")
		out.WriteString(n.Text)
		out.WriteString("-->")
		return nil

	case *ElementNode:
		if !ctx.root && n.staticFragment != "" {
			out.WriteString(n.staticFragment)
			return nil
		}
		return s.renderElement(out, n, ctx)

	default:
		return fmt.Errorf("unsupported AST node %T", node)
	}
}

func (s *renderState) renderElement(out *strings.Builder, element *ElementNode, ctx renderContext) error {
	if condition, exists := elementIfDirective(element); exists {
		value, err := evalExpression(condition, ctx.scope)
		if err != nil {
			return fmt.Errorf("<%s> m-if: %w", element.Tag, err)
		}
		if !truthy(value) {
			return nil
		}
	}

	if elementIsSlot(element) {
		if ctx.slot != nil {
			if ctx.slot.RawHTML != "" {
				out.WriteString(ctx.slot.RawHTML)
				return nil
			}
			return s.renderNodes(out, ctx.slot.Nodes, renderContext{
				scope:     ctx.slot.Scope,
				owner:     ctx.slot.Owner,
				root:      false,
				forceLazy: ctx.forceLazy,
			})
		}

		return s.renderNodes(out, element.Children, renderContext{
			scope:     ctx.scope,
			owner:     ctx.owner,
			root:      false,
			forceLazy: ctx.forceLazy,
		})
	}

	if elementIsClientOnly(element) {
		lazyHydration := "idle"
		for _, attr := range element.Attrs {
			if (attr.Name == "hydrate" || attr.Name == "client:hydrate") && !attr.Boolean && attributeEvalValue(attr) != "" {
				lazyHydration = strings.ToLower(attributeEvalValue(attr))
				break
			}
		}
		return s.renderNodes(out, element.Children, renderContext{
			scope: ctx.scope, slot: ctx.slot, owner: ctx.owner, root: false, forceLazy: true, lazyHydration: lazyHydration,
		})
	}

	if element.IsComponent {
		return s.renderComponentNode(out, element, ctx)
	}

	out.WriteByte('<')
	out.WriteString(element.Tag)
	revealClasses := elementRevealClasses(element)
	classWritten := false

	for _, attr := range element.Attrs {
		if attr.Event {
			eventName := strings.TrimPrefix(attr.Name, "@")
			eventName = strings.TrimPrefix(eventName, "v-on:")
			if eventName != "" {
				out.WriteString(" data-gosh-on-")
				out.WriteString(html.EscapeString(eventName))
				out.WriteString("=\"")
				out.WriteString(html.EscapeString(attr.Value))
				out.WriteByte('"')
			}
			continue
		}
		if attr.Prepared {
			if attr.SkipRender {
				continue
			}
		} else if shouldSkipRenderedAttribute(attr) {
			continue
		}

		name := attributeRenderedName(attr)
		if name == "" {
			continue
		}
		if attr.StaticHTML != "" && (name != "class" || revealClasses == "") {
			out.WriteString(attr.StaticHTML)
			continue
		}
		if name == "class" && revealClasses != "" {
			classWritten = true
			if attr.Dynamic {
				value, err := evalExpression(attributeEvalValue(attr), ctx.scope)
				if err != nil {
					return fmt.Errorf("<%s> %s: %w", element.Tag, attr.Name, err)
				}
				writeStaticHTMLAttribute(out, "class", Attribute{Value: appendHTMLClass(stringify(value), revealClasses)})
			} else {
				writeStaticHTMLAttribute(out, "class", Attribute{Value: appendHTMLClass(attr.Value, revealClasses)})
			}
			continue
		}

		if attr.Dynamic {
			value, err := evalExpression(attributeEvalValue(attr), ctx.scope)
			if err != nil {
				return fmt.Errorf("<%s> %s: %w", element.Tag, attr.Name, err)
			}
			if err := writeDynamicHTMLAttribute(out, name, value); err != nil {
				return fmt.Errorf("<%s> %s: %w", element.Tag, attr.Name, err)
			}
			continue
		}

		writeStaticHTMLAttribute(out, name, attr)
	}
	if revealClasses != "" && !classWritten {
		writeStaticHTMLAttribute(out, "class", Attribute{Value: revealClasses})
	}

	scopes := element.ScopeAttrs
	if ctx.root {
		if len(ctx.parentRootScopes) > 0 {
			scopes = append([]string(nil), scopes...)
		}
		for _, scopeID := range ctx.parentRootScopes {
			scopes = appendUnique(scopes, scopeID)
		}
	}
	for _, scopeID := range scopes {
		out.WriteByte(' ')
		out.WriteString(scopeID)
	}

	out.WriteByte('>')

	if isVoidHTMLTag(element.Tag) {
		return nil
	}
	if element.contextTemplate != nil {
		values := make(map[string]any, len(element.contextExpressions))
		for i, expression := range element.contextExpressions {
			value, err := evalExpression(expression, ctx.scope)
			if err != nil {
				return err
			}
			values["V"+strconv.Itoa(i)] = value
		}
		var content bytes.Buffer
		if err := element.contextTemplate.Execute(&content, values); err != nil {
			return err
		}
		body := content.Bytes()
		out.Write(body[len(element.Tag)+2 : len(body)-len(element.Tag)-3])
		out.WriteString("</" + element.Tag + ">")
		return nil
	}

	if err := s.renderNodes(out, element.Children, renderContext{
		scope:     ctx.scope,
		slot:      ctx.slot,
		owner:     ctx.owner,
		root:      false,
		forceLazy: ctx.forceLazy,
	}); err != nil {
		return err
	}

	out.WriteString("</")
	out.WriteString(element.Tag)
	out.WriteByte('>')
	return nil
}

var revealTypePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func revealSSRClasses(attrs []Attribute) string {
	typeName := ""
	speed := ""
	for _, attr := range attrs {
		if attr.Dynamic {
			continue
		}
		switch strings.ToLower(attr.Name) {
		case "data-gosh-reveal":
			typeName = strings.ToLower(strings.TrimSpace(attr.Value))
		case "data-gosh-reveal-speed":
			speed = strings.ToLower(strings.TrimSpace(attr.Value))
		}
	}
	if typeName == "" {
		return ""
	}
	if !revealTypePattern.MatchString(typeName) {
		typeName = "slide"
	}
	classes := "reveal-active reveal-" + typeName
	if speed == "fast" || speed == "slow" {
		classes += " reveal-" + speed
	}
	return classes
}

func appendHTMLClass(value, extra string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return extra
	}
	return value + " " + extra
}

func (s *renderState) renderComponentNode(out *strings.Builder, element *ElementNode, ctx renderContext) error {
	component, ok := s.renderer.components.Get(element.Tag)
	if !ok {
		return fmt.Errorf("component <%s> not found", element.Tag)
	}

	childScope := NewScopeCapacity(nil, len(element.Attrs))
	hydration := "visible"
	for _, attr := range element.Attrs {
		if attr.Name == "hydrate" || attr.Name == "client:hydrate" {
			if !attr.Boolean && attributeEvalValue(attr) != "" {
				hydration = strings.ToLower(attributeEvalValue(attr))
			}
			continue
		}
		if attr.Prepared {
			if attr.SkipRender {
				continue
			}
		} else if attr.Event || isStructuralDirective(attr.Name) || strings.HasPrefix(attr.Name, "#") {
			continue
		}

		name := attributeComponentProp(attr)
		if name == "" {
			continue
		}

		if attr.Dynamic {
			value, err := evalExpression(attributeEvalValue(attr), ctx.scope)
			if err != nil {
				return fmt.Errorf("<%s> prop %s: %w", element.Tag, attr.Name, err)
			}
			childScope.Set(name, value)
			continue
		}

		if attr.Boolean {
			childScope.Set(name, true)
		} else {
			childScope.Set(name, attr.Value)
		}
	}

	var slot *SlotContent
	if len(element.Children) > 0 {
		slot = &SlotContent{
			Nodes: element.Children,
			Scope: ctx.scope,
			Owner: ctx.owner,
		}
	}

	var parentScopes []string
	if ctx.owner != nil && ctx.owner.ScopeID != "" {
		parentScopes = append(parentScopes, ctx.owner.ScopeID)
	}
	if ctx.root {
		for _, scopeID := range ctx.parentRootScopes {
			parentScopes = appendUnique(parentScopes, scopeID)
		}
	}

	needsState := component.Mode == ModeClient || ctx.forceLazy || component.HasRuntimeAction
	var props Props
	encodedProps := ""
	if needsState {
		props = childScope.Snapshot()
		encodedProps = marshalProps(props)
	}
	s.instanceSeq++
	instance := makeInstanceID(component.Name, s.instanceSalt, s.instanceSeq, encodedProps)
	stateToken := ""
	if needsState {
		var err error
		stateToken, err = newRuntimeStateToken()
		if err != nil {
			return err
		}
		s.runtimeStates[stateToken] = cloneRuntimeProps(props)
	}

	out.WriteString(`<gosh-component class="gosh-component-root" data-gosh-component="`)
	out.WriteString(html.EscapeString(component.Name))
	out.WriteString(`" data-gosh-instance="`)
	out.WriteString(instance)
	if stateToken != "" {
		out.WriteString(`" data-gosh-state="`)
		out.WriteString(stateToken)
		out.WriteByte('"')
	} else {
		out.WriteByte('"')
	}

	if (component.Mode == ModeClient || ctx.forceLazy) && !s.forceClient {
		if ctx.forceLazy {
			hydration = ctx.lazyHydration
		}
		switch hydration {
		case "visible", "idle", "interaction", "immediate", "manual":
		default:
			hydration = "visible"
		}
		out.WriteString(` data-gosh-lazy="1" data-gosh-island="`)
		out.WriteString(hydration)
		out.WriteString(`">`)
		if hydration != "manual" {
			out.WriteString(s.renderer.loader)
		}
		out.WriteString(`</gosh-component>`)
		return nil
	}
	out.WriteByte('>')
	target := `[data-gosh-instance="` + instance + `"]`
	if err := s.renderComponent(out, component, childScope, slot, parentScopes, target); err != nil {
		return err
	}
	out.WriteString(`</gosh-component>`)
	return nil
}

func componentHasRuntimeAction(nodes []Node) bool {
	for _, node := range nodes {
		element, ok := node.(*ElementNode)
		if !ok {
			continue
		}
		for _, attr := range element.Attrs {
			if attr.Event || strings.HasPrefix(strings.ToLower(attr.Name), "data-gosh-on-") {
				return true
			}
		}
		if componentHasRuntimeAction(element.Children) {
			return true
		}
	}
	return false
}

func marshalProps(props Props) string {
	b, err := json.Marshal(props)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func makeInstanceID(name, salt string, seq uint64, encoded string) string {
	var input [128]byte
	value := input[:0]
	value = append(value, name...)
	value = append(value, ':')
	value = append(value, salt...)
	value = append(value, ':')
	value = strconv.AppendUint(value, seq, 10)
	value = append(value, ':')
	value = append(value, encoded...)
	sum := sha256.Sum256(value)
	return "gosh_" + hex.EncodeToString(sum[:6])
}

func findDirective(attrs []Attribute, name string) (Attribute, bool) {
	for _, attr := range attrs {
		if attr.Name == name {
			return attr, true
		}
	}
	return Attribute{}, false
}

func shouldSkipRenderedAttribute(attr Attribute) bool {
	if attr.Event {
		return true
	}
	if isStructuralDirective(attr.Name) {
		return true
	}
	if strings.HasPrefix(attr.Name, "#") {
		return true
	}
	return strings.HasPrefix(attr.Name, "v-") && !strings.HasPrefix(attr.Name, "v-bind:")
}

func isStructuralDirective(name string) bool {
	switch name {
	case "m-if", "m-else", "m-else-if", "m-for":
		return true
	default:
		return false
	}
}

func renderedAttributeName(name string) string {
	if strings.HasPrefix(name, ":") {
		return strings.TrimPrefix(name, ":")
	}
	if strings.HasPrefix(name, "v-bind:") {
		return strings.TrimPrefix(name, "v-bind:")
	}
	if strings.HasPrefix(name, "@") || strings.HasPrefix(name, "v-on:") {
		return ""
	}
	return name
}

func componentPropName(name string) string {
	name = renderedAttributeName(name)
	if name == "" || strings.HasPrefix(name, "v-") {
		return ""
	}
	return kebabToCamel(name)
}

func kebabToCamel(s string) string {
	var out strings.Builder
	upper := false
	for _, r := range s {
		if r == '-' {
			upper = true
			continue
		}
		if upper {
			out.WriteRune(unicode.ToUpper(r))
			upper = false
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func writeStaticHTMLAttribute(out *strings.Builder, name string, attr Attribute) {
	out.WriteByte(' ')
	out.WriteString(name)
	if attr.Boolean {
		return
	}
	out.WriteString(`="`)
	out.WriteString(html.EscapeString(attr.Value))
	out.WriteByte('"')
}

func staticHTMLAttribute(name string, attr Attribute) string {
	var out strings.Builder
	writeStaticHTMLAttribute(&out, name, attr)
	return out.String()
}

func writeDynamicHTMLAttribute(out *strings.Builder, name string, value any) error {
	lowerName := strings.ToLower(name)
	if strings.HasPrefix(lowerName, "on") && value != nil {
		if _, trusted := value.(stdtemplate.JS); !trusted {
			value = ""
		}
	}
	if lowerName == "srcdoc" && value != nil {
		if _, trusted := value.(stdtemplate.HTML); !trusted {
			value = html.EscapeString(stringify(value))
		}
	}
	switch strings.ToLower(name) {
	case "href", "src", "action", "formaction", "poster", "cite", "background", "xlink:href":
		_, trusted := value.(stdtemplate.URL)
		if value != nil && !trusted && !safeDynamicURL(stringify(value)) {
			value = "#ZgotmplZ"
		}
	}
	if value == nil || value == false {
		return nil
	}

	if b, ok := value.(bool); ok && b {
		out.WriteByte(' ')
		out.WriteString(name)
		return nil
	}

	out.WriteByte(' ')
	out.WriteString(name)
	out.WriteString(`="`)
	out.WriteString(html.EscapeString(stringify(value)))
	out.WriteByte('"')
	return nil
}

func evalExpression(expr string, scope *Scope) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}

	if strings.HasPrefix(expr, "!") {
		value, err := evalExpression(strings.TrimSpace(expr[1:]), scope)
		if err != nil {
			return nil, err
		}
		return !truthy(value), nil
	}

	if (strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`)) ||
		(strings.HasPrefix(expr, `'`) && strings.HasSuffix(expr, `'`)) {
		return expr[1 : len(expr)-1], nil
	}

	switch expr {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "undefined":
		return nil, nil
	}

	if looksLikeNumericLiteral(expr) {
		if n, err := strconv.ParseInt(expr, 10, 64); err == nil {
			return n, nil
		}
		if n, err := strconv.ParseFloat(expr, 64); err == nil {
			return n, nil
		}
	}

	name, remainder, hasMember := strings.Cut(expr, ".")
	value, ok := scope.Get(name)
	if !ok {
		return nil, nil
	}
	for hasMember {
		part, next, more := strings.Cut(remainder, ".")
		var found bool
		value, found = getMember(value, part)
		if !found {
			return nil, nil
		}
		remainder, hasMember = next, more
	}

	return value, nil
}

func looksLikeNumericLiteral(expr string) bool {
	if expr == "" {
		return false
	}
	first := expr[0]
	if first >= '0' && first <= '9' {
		return true
	}
	if first != '-' && first != '+' {
		return false
	}
	if len(expr) == 1 {
		return false
	}
	next := expr[1]
	return (next >= '0' && next <= '9') || next == '.'
}

func getMember(value any, name string) (any, bool) {
	switch object := value.(type) {
	case map[string]any:
		v, ok := object[name]
		return v, ok
	case Props:
		v, ok := object[name]
		return v, ok
	}
	return nil, false
}

func stringify(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(value)
	}
}

func truthy(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return true
	}
}

type Pages struct {
	root      string
	items     []*Page
	NotFound  *Page
	ErrorPage *Page
}

type tenantWeb struct {
	pages      *Pages
	content    map[string]contentPage
	components *Components
	layouts    *Components
	teleports  *Components
	renderer   *Renderer
}

type Page struct {
	Route        string
	RelativePath string
	Layout       string
	View         *Component
	segments     []routeSegment
	score        int
}

type routeSegment struct {
	kind routeSegmentKind
	name string
	text string
}

type routeSegmentKind uint8

const (
	routeStatic routeSegmentKind = iota
	routeDynamic
	routeCatchAll
)

func LoadPages(root string) (*Pages, error) {
	walkRoot := root
	baseRoot := root
	if _, prod := sourceFS(); !prod {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve pages root: %w", err)
		}
		walkRoot, baseRoot = absRoot, absRoot
	}
	pages := &Pages{root: baseRoot}

	err := sourceWalkDir(walkRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gosh") {
			return nil
		}

		relative, err := filepath.Rel(baseRoot, filePath)
		if err != nil {
			return err
		}

		view, err := ParseComponentFile(baseRoot, filePath)
		if err != nil {
			return fmt.Errorf("parse page %s: %w", filePath, err)
		}

		view.Name = "Page:" + filepath.ToSlash(relative)
		baseName := strings.ToLower(filepath.Base(relative))
		if baseName == "404.gosh" {
			pages.NotFound = &Page{Route: "@404", RelativePath: filepath.ToSlash(relative), Layout: view.Layout, View: view}
			return nil
		}
		if baseName == "error.gosh" {
			pages.ErrorPage = &Page{Route: "@error", RelativePath: filepath.ToSlash(relative), Layout: view.Layout, View: view}
			return nil
		}

		route, segments, score, err := pageRoute(relative)
		if err != nil {
			return fmt.Errorf("route for %s: %w", relative, err)
		}

		pages.items = append(pages.items, &Page{
			Route:        route,
			RelativePath: filepath.ToSlash(relative),
			Layout:       view.Layout,
			View:         view,
			segments:     segments,
			score:        score,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(pages.items, func(i, j int) bool {
		if pages.items[i].score != pages.items[j].score {
			return pages.items[i].score > pages.items[j].score
		}
		return pages.items[i].Route < pages.items[j].Route
	})

	return pages, nil
}

func (p *Pages) Len() int { return len(p.items) }

func (p *Pages) Routes() []string {
	out := make([]string, 0, len(p.items))
	for _, page := range p.items {
		out = append(out, page.Route)
	}
	sort.Strings(out)
	return out
}

func (p *Pages) Match(path string) (*Page, map[string]any, bool) {
	requestParts := splitURLPath(path)

	for _, page := range p.items {
		params, ok := matchRoute(page.segments, requestParts)
		if ok {
			return page, params, true
		}
	}

	return nil, nil, false
}

func (p *Pages) MatchStatic(path string) (*Page, map[string]any, bool) {
	for _, page := range p.items {
		if page.Route == path {
			return page, map[string]any{}, true
		}
	}
	return nil, nil, false
}

func pageRoute(relativePath string) (string, []routeSegment, int, error) {
	path := filepath.ToSlash(relativePath)
	path = strings.TrimSuffix(path, filepath.Ext(path))
	path = strings.TrimSuffix(path, ".client")
	path = strings.TrimSuffix(path, ".server")
	parts := strings.Split(path, "/")

	if len(parts) > 0 && parts[len(parts)-1] == "index" {
		parts = parts[:len(parts)-1]
	}

	segments := make([]routeSegment, 0, len(parts))
	routeParts := make([]string, 0, len(parts))
	score := 0

	for _, part := range parts {
		if part == "" {
			continue
		}

		if strings.HasPrefix(part, "[...") && strings.HasSuffix(part, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "[..."), "]")
			if name == "" {
				return "", nil, 0, fmt.Errorf("empty catch-all parameter")
			}
			segments = append(segments, routeSegment{kind: routeCatchAll, name: name})
			routeParts = append(routeParts, ":"+name+"*")
			score += 1
			continue
		}

		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "["), "]")
			if name == "" {
				return "", nil, 0, fmt.Errorf("empty dynamic parameter")
			}
			segments = append(segments, routeSegment{kind: routeDynamic, name: name})
			routeParts = append(routeParts, ":"+name)
			score += 10
			continue
		}

		segments = append(segments, routeSegment{kind: routeStatic, text: part})
		routeParts = append(routeParts, part)
		score += 100
	}

	if len(routeParts) == 0 {
		return "/", segments, score, nil
	}
	return "/" + strings.Join(routeParts, "/"), segments, score, nil
}

func splitURLPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func loadContent(root, publicPrefix string) (map[string]contentPage, error) {
	items := map[string]contentPage{}
	err := sourceWalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		data, err := sourceReadFile(file)
		if err != nil {
			return err
		}
		sourceRoot := strings.TrimSuffix(sourcePath(root), "/") + "/"
		sourceFile := sourcePath(file)
		if !strings.HasPrefix(sourceFile, sourceRoot) {
			return fmt.Errorf("content source %q is outside %q", file, root)
		}
		key := strings.TrimSuffix(strings.TrimPrefix(sourceFile, sourceRoot), ".md")
		page := parseMarkdownContent(string(data))
		page.Image = contentResourceURL(publicPrefix, filepath.ToSlash(filepath.Dir(key)), page.Image)
		items[key] = page
		return nil
	})
	if os.IsNotExist(err) {
		return items, nil
	}
	return items, err
}

func contentResourceURL(publicPrefix, contentDir, reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.HasPrefix(reference, "/") || strings.HasPrefix(reference, "//") || strings.HasPrefix(reference, "#") {
		return reference
	}
	if u, err := url.Parse(reference); err == nil && (u.IsAbs() || u.Host != "") {
		return reference
	}
	path, suffix, _ := strings.Cut(reference, "?")
	path = filepath.ToSlash(filepath.Clean(filepath.Join(contentDir, filepath.FromSlash(path))))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") {
		return reference
	}
	if suffix != "" {
		suffix = "?" + suffix
	}
	return strings.TrimRight(publicPrefix, "/") + "/" + strings.TrimLeft(path, "/") + suffix
}

func parseMarkdownContent(source string) contentPage {
	p := contentPage{}
	if strings.HasPrefix(source, "---") {
		if end := strings.Index(source[3:], "\n---"); end >= 0 {
			front := source[3 : end+3]
			for _, line := range strings.Split(front, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), "\"'")
				switch strings.TrimSpace(key) {
				case "title":
					p.Title = value
				case "description":
					p.Description = value
				case "image":
					p.Image = value
				}
			}
			source = strings.TrimLeft(source[end+7:], "\r\n")
		}
	}
	var out strings.Builder
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		escaped := html.EscapeString(line)
		switch {
		case strings.HasPrefix(line, "### "):
			out.WriteString("<h3>" + html.EscapeString(strings.TrimSpace(line[4:])) + "</h3>")
		case strings.HasPrefix(line, "## "):
			out.WriteString("<h2>" + html.EscapeString(strings.TrimSpace(line[3:])) + "</h2>")
		case strings.HasPrefix(line, "# "):
			out.WriteString("<h1>" + html.EscapeString(strings.TrimSpace(line[2:])) + "</h1>")
		default:
			out.WriteString("<p>" + escaped + "</p>")
		}
	}
	p.HTML = out.String()
	return p
}

func matchRoute(route []routeSegment, request []string) (map[string]any, bool) {
	params := make(map[string]any)
	i := 0

	for _, segment := range route {
		switch segment.kind {
		case routeStatic:
			if i >= len(request) || request[i] != segment.text {
				return nil, false
			}
			i++

		case routeDynamic:
			if i >= len(request) || request[i] == "" {
				return nil, false
			}
			params[segment.name] = request[i]
			i++

		case routeCatchAll:
			if i >= len(request) {
				return nil, false
			}
			parts := append([]string(nil), request[i:]...)
			params[segment.name] = strings.Join(parts, "/")
			params["__parts:"+segment.name] = parts
			i = len(request)
		}
	}

	if i != len(request) {
		return nil, false
	}
	return params, true
}

func publicRouteParams(params map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range params {
		if !strings.HasPrefix(k, "__parts:") {
			out[k] = v
		}
	}
	return out
}

func logicContext(r *http.Request, params map[string]any) *logic.Context {
	return logicContextWithPublicParams(r, params, publicRouteParams(params))
}

func logicContextWithPublicParams(r *http.Request, params, public map[string]any) *logic.Context {
	parts := map[string][]string{}
	for k, v := range params {
		if !strings.HasPrefix(k, "__parts:") {
			continue
		}
		if vv, ok := v.([]string); ok {
			parts[strings.TrimPrefix(k, "__parts:")] = append([]string(nil), vv...)
		}
	}
	return &logic.Context{Request: r, Path: r.URL.Path, Params: public, ParamPartsMap: parts}
}

func (a *App) localizeContext(ctx *logic.Context, r *http.Request) {
	if ctx == nil || r == nil || a.translate == nil {
		return
	}
	locale := ctx.Param("locale")
	ctx.Translator = func(key string) string {
		if locale != "" && a.translateLang != nil {
			return a.translateLang(key, locale)
		}
		return a.translate(r.Context(), key)
	}
}

func requestProps(r *http.Request, params map[string]any) Props {
	return requestPropsWithPublicParams(r, params, publicRouteParams(params))
}

func requestPropsWithPublicParams(r *http.Request, params, public map[string]any) Props {
	query := make(map[string]any)
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			query[key] = values[0]
		} else {
			copyValues := append([]string(nil), values...)
			query[key] = copyValues
		}
	}

	props := Props{
		"route": map[string]any{
			"path":   r.URL.Path,
			"params": clonePublicRouteParams(public),
			"query":  query,
		},
	}
	if locale, ok := params["locale"].(string); ok && locale != "" {
		props["locale"] = locale
		props["lang"] = locale
	}
	return props
}

func clonePublicRouteParams(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func sourceFS() (fs.FS, bool) { return productionSourceFS() }

func playgroundPath(name string) (string, bool) {
	if _, production := sourceFS(); production || !playgroundEnabled.Load() {
		return "", false
	}
	webRoot, err := filepath.Abs("web")
	if err != nil {
		return "", false
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(webRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	if relative == "tenants" || strings.HasPrefix(relative, "tenants"+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(playgroundDir, relative), true
}

func sourceFilePath(name string) string {
	if candidate, ok := playgroundPath(name); ok {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return name
}

func sourcePath(name string) string {
	name = filepath.ToSlash(filepath.Clean(name))
	name = strings.TrimPrefix(name, "./")
	return name
}

func sourceReadFile(name string) ([]byte, error) {
	if f, ok := sourceFS(); ok {
		return fs.ReadFile(f, sourcePath(name))
	}
	return os.ReadFile(sourceFilePath(name))
}

func sourceReadDir(name string) ([]fs.DirEntry, error) {
	if f, ok := sourceFS(); ok {
		return fs.ReadDir(f, sourcePath(name))
	}
	items := map[string]fs.DirEntry{}
	entries, baseErr := os.ReadDir(name)
	if baseErr == nil {
		for _, entry := range entries {
			items[entry.Name()] = entry
		}
	}
	if overlay, ok := playgroundPath(name); ok {
		entries, overlayErr := os.ReadDir(overlay)
		if overlayErr == nil {
			for _, entry := range entries {
				items[entry.Name()] = entry
			}
		} else if !os.IsNotExist(overlayErr) {
			return nil, overlayErr
		}
	}
	if len(items) == 0 && baseErr != nil {
		return nil, baseErr
	}
	out := make([]fs.DirEntry, 0, len(items))
	for _, entry := range items {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func sourceWalkDir(root string, fn fs.WalkDirFunc) error {
	if f, ok := sourceFS(); ok {
		return fs.WalkDir(f, sourcePath(root), fn)
	}
	paths := map[string]fs.DirEntry{}
	collect := func(physical string, replace bool) error {
		return filepath.WalkDir(physical, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(physical, path)
			if err != nil {
				return err
			}
			if _, exists := paths[relative]; !exists || replace {
				paths[relative] = entry
			}
			return nil
		})
	}
	baseErr := collect(root, false)
	if overlay, ok := playgroundPath(root); ok {
		if err := collect(overlay, true); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if len(paths) == 0 && baseErr != nil {
		return baseErr
	}
	ordered := make([]string, 0, len(paths))
	for relative := range paths {
		ordered = append(ordered, relative)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i] == "." {
			return true
		}
		if ordered[j] == "." {
			return false
		}
		return ordered[i] < ordered[j]
	})
	for _, relative := range ordered {
		if err := fn(filepath.Join(root, relative), paths[relative], nil); err != nil {
			return err
		}
	}
	return nil
}

type BasePageData struct {
	Variables     map[string]any
	GlobalHead    stdtemplate.HTML
	PageHead      stdtemplate.HTML
	GlobalStyles  stdtemplate.HTML
	PageStyles    stdtemplate.HTML
	GlobalScripts stdtemplate.HTML
	PageScripts   stdtemplate.HTML
	Content       stdtemplate.HTML
}

func applyFullscreenPreloaderData(res *RenderResult, ctx *logic.Context) {
	if res.Data == nil {
		res.Data = Props{}
	}
	state := ctx.FullscreenPreloader()
	enabled, transparent := false, false
	if state.Enabled != nil {
		enabled = *state.Enabled
	}
	if state.Transparent != nil {
		transparent = *state.Transparent
	}
	background, backgroundDark, label := state.Background, state.BackgroundDark, state.AriaLabel
	if background == "" {
		background = "var(--ui-bg)"
	}
	if backgroundDark == "" {
		backgroundDark = background
	}
	if label == "" {
		label = "Page loading"
	}
	zIndex, minimumDuration := 9999, 250
	if state.ZIndex != nil {
		zIndex = *state.ZIndex
	}
	if state.MinimumDuration != nil {
		minimumDuration = *state.MinimumDuration
	}
	res.Data["preloaderEnabled"] = enabled
	res.Data["preloaderTransparent"] = transparent
	res.Data["preloaderBackground"] = background
	res.Data["preloaderBackgroundDark"] = backgroundDark
	res.Data["preloaderZIndex"] = zIndex
	res.Data["preloaderAriaLabel"] = label
	res.Data["preloaderMinimumDuration"] = minimumDuration
}

type GlobalInserts struct{ Head, Styles, Scripts string }

type cacheEntry struct {
	Result      RenderResult
	Document    *cachedDocument
	Stored      time.Time
	TagVersions map[string]string
}

type cachedDocument struct {
	Plan         ModulePlan
	Body         []byte
	States       map[string]Props
	PublicStatic bool
}

const (
	cachedDocumentNonceMarker = "__GOSH_CSP_NONCE__"
	cachedDocumentRootMarker  = "__GOSH_ROOT_STATE__"
	cachedDocumentStatePrefix = "__GOSH_STATE_"
)

type renderCache struct {
	mu               sync.RWMutex
	items            map[string]cacheEntry
	revalidating     map[string]bool
	flights          singleflight.Group
	maxEntries       int
	documentBytes    int
	maxDocumentBytes int
	tags             map[string]map[string]bool
	keyTags          map[string]map[string]bool
}

func newRenderCache(max int) *renderCache {
	if max <= 0 {
		max = 1024
	}
	return &renderCache{
		items:            map[string]cacheEntry{},
		revalidating:     map[string]bool{},
		maxEntries:       max,
		maxDocumentBytes: 32 << 20,
		tags:             map[string]map[string]bool{},
		keyTags:          map[string]map[string]bool{},
	}
}
func (c *renderCache) get(key string) (cacheEntry, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	return e, ok
}

func (c *renderCache) setDocument(key string, document *cachedDocument) {
	if document == nil || len(document.Body) > 1<<20 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok {
		return
	}
	if entry.Document != nil {
		c.documentBytes -= len(entry.Document.Body)
	}
	for c.documentBytes+len(document.Body) > c.maxDocumentBytes {
		removed := false
		for candidate, cached := range c.items {
			if candidate == key || cached.Document == nil {
				continue
			}
			c.documentBytes -= len(cached.Document.Body)
			cached.Document = nil
			c.items[candidate] = cached
			removed = true
			break
		}
		if !removed {
			return
		}
	}
	entry.Document = document
	c.items[key] = entry
	c.documentBytes += len(document.Body)
}
func (c *renderCache) set(key string, result RenderResult) {
	c.setWithTagVersions(key, result, nil)
}

func (c *renderCache) setWithTagVersions(key string, result RenderResult, tagVersions map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.maxEntries {
		var oldest string
		var t time.Time
		for k, e := range c.items {
			if oldest == "" || e.Stored.Before(t) {
				oldest = k
				t = e.Stored
			}
		}
		if evicted := c.items[oldest]; evicted.Document != nil {
			c.documentBytes -= len(evicted.Document.Body)
		}
		delete(c.items, oldest)
	}
	if previous, found := c.items[key]; found && previous.Document != nil {
		c.documentBytes -= len(previous.Document.Body)
	}
	versions := make(map[string]string, len(tagVersions))
	for tag, version := range tagVersions {
		versions[tag] = version
	}
	c.items[key] = cacheEntry{Result: result, Stored: time.Now(), TagVersions: versions}
	if old := c.keyTags[key]; old != nil {
		for tag := range old {
			delete(c.tags[tag], key)
			if len(c.tags[tag]) == 0 {
				delete(c.tags, tag)
			}
		}
	}
	set := map[string]bool{}
	for _, tag := range result.CacheTags {
		if tag == "" {
			continue
		}
		set[tag] = true
		if c.tags[tag] == nil {
			c.tags[tag] = map[string]bool{}
		}
		c.tags[tag][key] = true
	}
	c.keyTags[key] = set
}
func (c *renderCache) invalidateTags(tags []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, tag := range tags {
		for key := range c.tags[tag] {
			if entry := c.items[key]; entry.Document != nil {
				c.documentBytes -= len(entry.Document.Body)
			}
			delete(c.items, key)
			delete(c.keyTags, key)
		}
		delete(c.tags, tag)
	}
}
func (c *renderCache) beginRevalidate(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.revalidating[key] {
		return false
	}
	c.revalidating[key] = true
	return true
}
func (c *renderCache) endRevalidate(key string) {
	c.mu.Lock()
	delete(c.revalidating, key)
	c.mu.Unlock()
}

type cachedHTTPResponse struct {
	Status int
	Header http.Header
	Body   []byte
	Stored time.Time
}
type httpResponseCache struct {
	mu           sync.RWMutex
	items        map[string]cachedHTTPResponse
	revalidating map[string]bool
	flights      singleflight.Group
	maxEntries   int
}

func newHTTPResponseCache(max int) *httpResponseCache {
	if max <= 0 {
		max = 512
	}
	return &httpResponseCache{items: map[string]cachedHTTPResponse{}, revalidating: map[string]bool{}, maxEntries: max}
}
func (c *httpResponseCache) get(k string) (cachedHTTPResponse, bool) {
	c.mu.RLock()
	e, ok := c.items[k]
	c.mu.RUnlock()
	return e, ok
}
func (c *httpResponseCache) set(k string, e cachedHTTPResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.maxEntries {
		for old := range c.items {
			delete(c.items, old)
			break
		}
	}
	c.items[k] = e
}
func (c *httpResponseCache) begin(k string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.revalidating[k] {
		return false
	}
	c.revalidating[k] = true
	return true
}
func (c *httpResponseCache) end(k string) { c.mu.Lock(); delete(c.revalidating, k); c.mu.Unlock() }

type captureWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{header: make(http.Header), status: http.StatusOK}
}
func (w *captureWriter) Header() http.Header { return w.header }
func (w *captureWriter) WriteHeader(code int) {
	if w.status == http.StatusOK {
		w.status = code
	}
}
func (w *captureWriter) Write(p []byte) (int, error) { return w.body.Write(p) }
func (w *captureWriter) Flush()                      {}
func cloneHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}
func writeCachedHTTP(w http.ResponseWriter, e cachedHTTPResponse, state string) {
	for k, v := range e.Header {
		w.Header()[k] = append([]string(nil), v...)
	}
	w.Header().Set("X-MyelophOne-Cache", state)
	w.WriteHeader(e.Status)
	_, _ = w.Write(e.Body)
}

type App struct {
	components         *Components
	teleports          *Components
	pages              *Pages
	layouts            *Components
	config             logic.RuntimeConfig
	renderer           *Renderer
	base               *stdtemplate.Template
	baseShell          documentShell
	runtimeConfigHTML  string
	browserWarningHTML string
	skipLinkHTML       string
	preloaderHTML      string
	globalScriptsHTML  string
	globals            GlobalInserts
	modules            map[string]string
	chunks             map[string]string
	styles             map[string]string
	finalCSS           map[string]string
	precompiledStyles  map[string][]string
	scriptUsage        map[string]int
	sourceChunks       map[string]map[string]string
	tailwindCSS        map[string]string
	commonCSS          string
	unifiedCSS         string
	clientEntry        []byte
	entrySources       map[string]bool
	clientURL          string
	vitalsURL          string
	webSocketChunkURL  string
	spaLoadingTemplate string
	routeCache         *renderCache
	componentDataCache *renderCache
	apiCache           *httpResponseCache
	runtimeState       RuntimeStateStore
	pagePlans          map[string]ModulePlan
	pagePlanExpiry     map[string]time.Time
	pagePlanOrder      []string
	modulePlans        sync.Map
	webCache           CacheStore
	content            map[string]contentPage
	baseCSS            string
	tenantMu           sync.RWMutex
	tenants            map[string]*tenantWeb
	tenantFlights      singleflight.Group
	translate          func(context.Context, string) string
	translateLang      func(string, string) string
	responder          logic.Server
	publicFileHandler  func(http.ResponseWriter, *http.Request)
	errorRenderer      func(http.ResponseWriter, *http.Request, int, string)
	mu                 sync.Mutex
	siteSearchMu       sync.RWMutex
	siteSearchIndex    []siteSearchDocument
}

type contentPage struct{ Title, Description, Image, HTML string }

type documentShell struct {
	parts [][]byte
}

type standaloneWebResponder struct{}

func (standaloneWebResponder) RespondJSON(w http.ResponseWriter, _ *http.Request, data any) {
	_ = RespondJSONFast(w, data)
}
func (standaloneWebResponder) RespondHTML(w http.ResponseWriter, content string) {
	_ = RespondHTMLFast(w, content)
}
func (standaloneWebResponder) RespondText(w http.ResponseWriter, content string) {
	_ = RespondTextFast(w, content)
}
func (standaloneWebResponder) RespondRawJSON(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}
func (standaloneWebResponder) RespondSecureJSON(w http.ResponseWriter, _ *http.Request, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = io.WriteString(w, "while(1);")
	_ = json.NewEncoder(w).Encode(data)
}
func (standaloneWebResponder) RespondXML(w http.ResponseWriter, _ *http.Request, data any) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_ = xml.NewEncoder(w).Encode(data)
}
func (standaloneWebResponder) RespondAsciiJSON(w http.ResponseWriter, _ *http.Request, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var buf bytes.Buffer
	if json.NewEncoder(&buf).Encode(data) == nil {
		_, _ = w.Write(toASCII(buf.Bytes()))
	}
}

type Frame map[string]any

const generatedAssetBanner = "/* © 2025 Aliaksandr Ivanou (aleksivanov.me). All rights reserved */\n"

func withGeneratedAssetBanner(source string) string {
	if strings.HasPrefix(source, generatedAssetBanner) {
		return source
	}
	return generatedAssetBanner + source
}

func withGeneratedJSBanner(body []byte) []byte {
	if bytes.HasPrefix(body, []byte(generatedAssetBanner)) {
		return body
	}
	return append([]byte(generatedAssetBanner), body...)
}

type actionRequest struct {
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Action   string         `json:"action"`
	Instance string         `json:"instance"`
	State    string         `json:"state"`
	URL      string         `json:"url"`
	Fields   map[string]any `json:"fields,omitempty"`
}

const (
	runtimeStateCookie = "gosh_runtime"
	runtimeStateTTL    = 30 * time.Minute
	runtimeStateLimit  = 4096
	runtimeStatePrune  = time.Minute
)

type RuntimeStateStore interface {
	Put(owner string, props Props) (token string, err error)
	Get(owner, token string) (props Props, ok bool)
}

type trustedRuntimeStateStore interface {
	PutTrusted(owner string, props Props) (token string, err error)
}

type runtimeStateStore struct {
	mu        sync.Mutex
	entries   map[string]runtimeStateEntry
	order     []string
	nextPrune time.Time
}

type runtimeStateEntry struct {
	owner   string
	props   Props
	expires time.Time
}

func newRuntimeStateStore() *runtimeStateStore {
	return &runtimeStateStore{entries: make(map[string]runtimeStateEntry)}
}

func newRuntimeStateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func cloneRuntimeProps(props Props) Props {
	if len(props) == 0 {
		return Props{}
	}
	encoded, err := json.Marshal(props)
	if err == nil {
		var cloned Props
		if json.Unmarshal(encoded, &cloned) == nil && cloned != nil {
			return cloned
		}
	}
	cloned := make(Props, len(props))
	for key, value := range props {
		cloned[key] = value
	}
	return cloned
}

func (s *runtimeStateStore) discardOldestLocked() bool {
	for len(s.order) > 0 {
		token := s.order[0]
		s.order = s.order[1:]
		if _, found := s.entries[token]; found {
			delete(s.entries, token)
			return true
		}
	}
	return false
}

func (s *runtimeStateStore) pruneExpiredLocked(now time.Time) {
	for len(s.order) > 0 {
		token := s.order[0]
		entry, found := s.entries[token]
		if !found {
			s.order = s.order[1:]
			continue
		}
		if now.Before(entry.expires) {
			break
		}
		delete(s.entries, token)
		s.order = s.order[1:]
	}
	s.nextPrune = now.Add(runtimeStatePrune)
}

func (s *runtimeStateStore) put(owner string, props Props, trusted bool) (string, error) {
	token, err := newRuntimeStateToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	if !trusted {
		props = cloneRuntimeProps(props)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextPrune.IsZero() || !now.Before(s.nextPrune) {
		s.pruneExpiredLocked(now)
	}
	for len(s.entries) >= runtimeStateLimit && s.discardOldestLocked() {
	}
	s.entries[token] = runtimeStateEntry{owner: owner, props: props, expires: now.Add(runtimeStateTTL)}
	s.order = append(s.order, token)
	return token, nil
}

func (s *runtimeStateStore) Put(owner string, props Props) (string, error) {
	return s.put(owner, props, false)
}

func (s *runtimeStateStore) PutTrusted(owner string, props Props) (string, error) {
	return s.put(owner, props, true)
}

func putCachedRuntimeState(store RuntimeStateStore, owner string, props Props) (string, error) {
	if trusted, ok := store.(trustedRuntimeStateStore); ok {
		return trusted.PutTrusted(owner, props)
	}
	return store.Put(owner, props)
}

func (s *runtimeStateStore) Get(owner, token string) (Props, bool) {
	if token == "" || len(token) > 128 {
		return nil, false
	}
	s.mu.Lock()
	entry, ok := s.entries[token]
	if !ok || entry.owner != owner || !time.Now().Before(entry.expires) {
		if ok && !time.Now().Before(entry.expires) {
			delete(s.entries, token)
		}
		s.mu.Unlock()
		return nil, false
	}
	s.mu.Unlock()
	return cloneRuntimeProps(entry.props), true
}

func (a *App) SetRuntimeStateStore(store RuntimeStateStore) {
	if store != nil {
		a.runtimeState = store
	}
}

func (a *App) runtimeOwner(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(runtimeStateCookie); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}
	owner, err := newRuntimeStateToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: runtimeStateCookie, Value: owner, Path: "/", MaxAge: int(runtimeStateTTL.Seconds()), HttpOnly: true, Secure: !strings.EqualFold(a.config.Environment, "development"), SameSite: http.SameSiteLaxMode})
	return owner, nil
}

func (a *App) bindRuntimeState(w http.ResponseWriter, r *http.Request, result *RenderResult) (string, error) {
	owner, err := a.runtimeOwner(w, r)
	if err != nil {
		return "", err
	}
	replacements := make(map[string]string, len(result.RuntimeStates))
	for previous, props := range result.RuntimeStates {
		next, err := a.runtimeState.Put(owner, props)
		if err != nil {
			return "", err
		}
		replacements[previous] = next
	}
	result.HTML = replaceRuntimeStateTokens(result.HTML, replacements)
	result.TeleportHTML = replaceRuntimeStateTokens(result.TeleportHTML, replacements)
	return a.runtimeState.Put(owner, result.Data)
}

func (a *App) buildCachedDocument(page *Page, status int, result RenderResult, publicStatic bool) (*cachedDocument, error) {
	if status != http.StatusOK {
		return nil, nil
	}
	templateResult := result
	if publicStatic {
		templateResult.HTML = removeRuntimeStateAttributes(templateResult.HTML)
		templateResult.TeleportHTML = removeRuntimeStateAttributes(templateResult.TeleportHTML)
		body, err := a.renderBase(templateResult, page, status, "", "", true)
		if err != nil {
			return nil, err
		}
		return &cachedDocument{Body: body, PublicStatic: true}, nil
	}
	states := make(map[string]Props, len(result.RuntimeStates)+1)
	states[cachedDocumentRootMarker] = result.Data
	replacements := make(map[string]string, len(result.RuntimeStates))
	for source, props := range result.RuntimeStates {
		marker := cachedDocumentStatePrefix + strconv.Itoa(len(states)) + "__"
		replacements[source] = marker
		states[marker] = props
	}
	templateResult.HTML = replaceRuntimeStateTokens(templateResult.HTML, replacements)
	templateResult.TeleportHTML = replaceRuntimeStateTokens(templateResult.TeleportHTML, replacements)
	body, err := a.renderBase(templateResult, page, status, cachedDocumentNonceMarker, cachedDocumentRootMarker, false)
	if err != nil {
		return nil, err
	}
	plan := result.RuntimePlan
	if plan.Empty() && len(result.Scripts) > 0 {
		plan = a.modulePlan(page.RelativePath, result.Scripts, true)
	}
	return &cachedDocument{Body: body, States: states, Plan: plan}, nil
}

func replaceRuntimeStateTokens(source string, replacements map[string]string) string {
	if source == "" || len(replacements) == 0 {
		return source
	}
	const prefix = `data-gosh-state="`
	var out strings.Builder
	out.Grow(len(source))
	for {
		start := strings.Index(source, prefix)
		if start < 0 {
			out.WriteString(source)
			return out.String()
		}
		valueStart := start + len(prefix)
		endOffset := strings.IndexByte(source[valueStart:], '"')
		if endOffset < 0 {
			out.WriteString(source)
			return out.String()
		}
		valueEnd := valueStart + endOffset
		out.WriteString(source[:valueStart])
		if replacement, ok := replacements[source[valueStart:valueEnd]]; ok {
			out.WriteString(replacement)
		} else {
			out.WriteString(source[valueStart:valueEnd])
		}
		out.WriteByte('"')
		source = source[valueEnd+1:]
	}
}

func removeRuntimeStateAttributes(source string) string {
	const prefix = ` data-gosh-state="`
	for {
		start := strings.Index(source, prefix)
		if start < 0 {
			return source
		}
		end := strings.IndexByte(source[start+len(prefix):], '"')
		if end < 0 {
			return source
		}
		end += start + len(prefix) + 1
		source = source[:start] + source[end:]
	}
}

func (a *App) materializeCachedDocument(w http.ResponseWriter, r *http.Request, document *cachedDocument) ([]byte, error) {
	if document == nil {
		return nil, errors.New("nil cached document")
	}
	if document.PublicStatic {
		setPublicStaticCSPHeaders(w)
		return document.Body, nil
	}
	owner, err := a.runtimeOwner(w, r)
	if err != nil {
		return nil, err
	}
	replacements := make(map[string]string, len(document.States)+1)
	for marker, props := range document.States {
		token, err := putCachedRuntimeState(a.runtimeState, owner, props)
		if err != nil {
			return nil, err
		}
		replacements[marker] = token
		if marker == cachedDocumentRootMarker {
			a.rememberPagePlan(token, document.Plan)
		}
	}
	nonce, err := newCSPNonce()
	if err != nil {
		return nil, err
	}
	setCSPHeaders(w, nonce)
	replacements[cachedDocumentNonceMarker] = nonce
	return replaceCachedDocumentMarkers(document.Body, replacements), nil
}

func replaceCachedDocumentMarkers(body []byte, replacements map[string]string) []byte {
	if len(body) == 0 || len(replacements) == 0 {
		return body
	}
	out := make([]byte, 0, len(body))
	const markerPrefix = "__GOSH_"
	for offset := 0; offset < len(body); {
		at := bytes.Index(body[offset:], []byte(markerPrefix))
		if at < 0 {
			return append(out, body[offset:]...)
		}
		at += offset
		out = append(out, body[offset:at]...)
		endAt := bytes.Index(body[at+len(markerPrefix):], []byte("__"))
		if endAt < 0 {
			return append(out, body[at:]...)
		}
		endAt += at + len(markerPrefix) + 2
		marker := string(body[at:endAt])
		if replacement, ok := replacements[marker]; ok {
			out = append(out, replacement...)
		} else {
			out = append(out, body[at:endAt]...)
		}
		offset = endAt
	}
	return out
}

func templateVar(data any, key string) any {
	switch v := data.(type) {
	case BasePageData:
		return v.Variables[key]
	case *BasePageData:
		if v != nil {
			return v.Variables[key]
		}
	case map[string]any:
		return v[key]
	}
	return nil
}

func templateDefault(fallback, value any) any {
	if value == nil {
		return fallback
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return fallback
		}
	}
	return value
}

func loadBaseTemplate() (*stdtemplate.Template, error) {
	return stdtemplate.New("base.html").Funcs(stdtemplate.FuncMap{
		"var":     templateVar,
		"default": templateDefault,
	}).Parse(string(embeddedBase) + `{{define "content"}}{{.Content}}{{end}}`)
}

const (
	shellLangMarker        = "__GOSH_SHELL_LANG__"
	shellTitleMarker       = "__GOSH_SHELL_TITLE__"
	shellDescriptionMarker = "__GOSH_SHELL_DESCRIPTION__"
	shellGlobalHeadMarker  = "__GOSH_SHELL_GLOBAL_HEAD__"
	shellPageHeadMarker    = "__GOSH_SHELL_PAGE_HEAD__"
	shellGlobalStylesMark  = "__GOSH_SHELL_GLOBAL_STYLES__"
	shellPageStylesMarker  = "__GOSH_SHELL_PAGE_STYLES__"
	shellContentMarker     = "__GOSH_SHELL_CONTENT__"
	shellGlobalScriptsMark = "__GOSH_SHELL_GLOBAL_SCRIPTS__"
	shellPageScriptsMarker = "__GOSH_SHELL_PAGE_SCRIPTS__"
)

func buildBaseShell(base *stdtemplate.Template) (documentShell, error) {
	if base == nil {
		return documentShell{}, errors.New("nil base template")
	}
	data := BasePageData{
		Variables: map[string]any{
			"lang":        shellLangMarker,
			"title":       shellTitleMarker,
			"description": shellDescriptionMarker,
		},
		GlobalHead:    stdtemplate.HTML(shellGlobalHeadMarker),
		PageHead:      stdtemplate.HTML(shellPageHeadMarker),
		GlobalStyles:  stdtemplate.HTML(shellGlobalStylesMark),
		PageStyles:    stdtemplate.HTML(shellPageStylesMarker),
		GlobalScripts: stdtemplate.HTML(shellGlobalScriptsMark),
		PageScripts:   stdtemplate.HTML(shellPageScriptsMarker),
		Content:       stdtemplate.HTML(shellContentMarker),
	}
	var buf bytes.Buffer
	if err := base.ExecuteTemplate(&buf, "base", data); err != nil {
		return documentShell{}, err
	}
	raw := buf.Bytes()
	markers := []string{
		shellLangMarker, shellTitleMarker, shellDescriptionMarker,
		shellGlobalHeadMarker, shellPageHeadMarker, shellGlobalStylesMark,
		shellPageStylesMarker, shellContentMarker, shellGlobalScriptsMark,
		shellPageScriptsMarker,
	}
	parts := make([][]byte, 0, len(markers)+1)
	offset := 0
	for _, marker := range markers {
		at := bytes.Index(raw[offset:], []byte(marker))
		if at < 0 {
			return documentShell{}, fmt.Errorf("base shell marker %q not found", marker)
		}
		at += offset
		parts = append(parts, raw[offset:at])
		offset = at + len(marker)
	}
	parts = append(parts, raw[offset:])
	return documentShell{parts: parts}, nil
}

func materializeBaseShell(shell documentShell, data BasePageData) []byte {
	values := [...]string{
		html.EscapeString(fmt.Sprint(data.Variables["lang"])),
		html.EscapeString(fmt.Sprint(data.Variables["title"])),
		html.EscapeString(fmt.Sprint(data.Variables["description"])),
		string(data.GlobalHead), string(data.PageHead), string(data.GlobalStyles),
		string(data.PageStyles), string(data.Content), string(data.GlobalScripts),
		string(data.PageScripts),
	}
	out := make([]byte, 0, len(shell.parts[0])+len(data.Content))
	for i, value := range values {
		out = append(out, shell.parts[i]...)
		out = append(out, value...)
	}
	return append(out, shell.parts[len(values)]...)
}

func loadGlobalInserts(root string) (GlobalInserts, error) {
	read := func(dir string) (string, error) {
		entries, err := sourceReadDir(filepath.Join(root, dir))
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		var out strings.Builder
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".gosh") {
				continue
			}
			b, err := sourceReadFile(filepath.Join(root, dir, e.Name()))
			if err != nil {
				return "", err
			}
			out.Write(b)
			out.WriteByte('\n')
		}
		return out.String(), nil
	}
	head, err := read("head")
	if err != nil {
		return GlobalInserts{}, err
	}
	styles, err := read("styles")
	if err != nil {
		return GlobalInserts{}, err
	}
	scripts, err := read("scripts")
	if err != nil {
		return GlobalInserts{}, err
	}
	return GlobalInserts{Head: head, Styles: styles, Scripts: scripts}, nil
}

func loadLoader(path string) string {
	b, err := sourceReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return defaultLoaderHTML
	}
	blocks, err := parseSFCBlocks(string(b))
	if err == nil {
		for _, block := range blocks {
			if block.Name == "template" {
				return block.Block.Content
			}
		}
	}
	return string(b)
}

var localCSSImportPattern = regexp.MustCompile(`(?i)@import\s+(?:url\(\s*)?(?:"([^"]+)"|'([^']+)')\s*\)?\s*;`)

func loadOptionalCSS(path string) (string, error) {
	css, _, err := loadCSSWithImports(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return css, err
}

func loadDefaultCSS() (string, error) {
	systemCSS, err := loadOptionalCSS(filepath.Join(systemFrameworkCSSDir, "default.css"))
	if err != nil {
		return "", fmt.Errorf("load system default CSS: %w", err)
	}
	projectCSS, err := loadOptionalCSS(filepath.Join(systemWebCSSDir, "default.css"))
	if err != nil {
		return "", fmt.Errorf("load project default CSS: %w", err)
	}
	return combineDefaultCSS(systemCSS, projectCSS), nil
}

func combineDefaultCSS(systemCSS, projectCSS string) string {
	return systemCSS + "\n" + projectCSS
}

func loadCSSWithImports(path string) (string, []string, error) {
	root := filepath.Dir(path)
	files := make(map[string]bool)
	active := make(map[string]bool)
	css, err := loadCSSImportFile(filepath.Clean(path), root, active, files)
	if err != nil {
		return "", nil, err
	}
	paths := make([]string, 0, len(files))
	for file := range files {
		paths = append(paths, file)
	}
	sort.Strings(paths)
	return css, paths, nil
}

func loadCSSImportFile(path, root string, active, files map[string]bool) (string, error) {
	if !cssPathWithin(path, root) {
		return "", fmt.Errorf("CSS import %q is outside %q", path, root)
	}
	path = filepath.Clean(path)
	if active[path] {
		return "", fmt.Errorf("CSS import cycle at %q", path)
	}
	data, err := sourceReadFile(path)
	if err != nil {
		return "", err
	}
	files[path] = true
	active[path] = true
	defer delete(active, path)

	var importErr error
	css := localCSSImportPattern.ReplaceAllStringFunc(string(data), func(rule string) string {
		if importErr != nil {
			return rule
		}
		match := localCSSImportPattern.FindStringSubmatch(rule)
		reference := match[1]
		if reference == "" {
			reference = match[2]
		}
		if isExternalCSSReference(reference) {
			return rule
		}
		fileRef := strings.SplitN(strings.SplitN(reference, "#", 2)[0], "?", 2)[0]
		if !strings.EqualFold(filepath.Ext(fileRef), ".css") {
			importErr = fmt.Errorf("CSS import %q must reference a .css file", reference)
			return rule
		}
		imported, err := loadCSSImportFile(filepath.Join(filepath.Dir(path), filepath.FromSlash(fileRef)), root, active, files)
		if err != nil {
			importErr = err
			return rule
		}
		return "/* imported " + filepath.ToSlash(fileRef) + " */\n" + imported
	})
	if importErr != nil {
		return "", importErr
	}
	return css, nil
}

func cssPathWithin(path, root string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isExternalCSSReference(reference string) bool {
	lower := strings.ToLower(strings.TrimSpace(reference))
	return strings.HasPrefix(lower, "//") || strings.Contains(lower, "://") || strings.HasPrefix(lower, "data:")
}

func loadSPALoadingTemplate(enabled bool, loggers ...*log.Logger) string {
	if !enabled {
		return ""
	}
	data, err := sourceReadFile(filepath.Join(systemTemplatesDir, "spa-loading-template.html"))
	if err != nil {
		if !os.IsNotExist(err) {
			r := &Renderer{}
			if len(loggers) > 0 {
				r.logger = loggers[0]
			}
			r.logError("load SPA loading template: %v", err)
		}
		return ""
	}
	return string(data)
}

func NewApp() (*App, error) {
	return newAppWithLogger(nil)
}

func newAppWithLogger(logger *log.Logger) (*App, error) {
	cfg, err := logic.UseRuntimeConfig()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	playgroundEnabled.Store(strings.EqualFold(cfg.Environment, "Development"))
	if err := logic.SetupModules(cfg); err != nil {
		return nil, err
	}
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		return nil, err
	}
	teleports, err := LoadComponents(systemTeleportDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if teleports == nil {
		teleports = &Components{items: map[string]*Component{}}
	}
	systemTeleports, err := LoadComponents(systemFrameworkTeleportDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if systemTeleports != nil {
		for _, name := range systemTeleports.Names() {
			component, _ := systemTeleports.Get(name)
			key := normalizeComponentLookup(name)
			if _, exists := teleports.items[key]; exists {
				return nil, fmt.Errorf("system teleport component %q conflicts with a user teleport", name)
			}
			teleports.items[key] = component
		}
		teleports.names = componentNames(teleports.items)
	}
	for _, name := range teleports.Names() {
		component, _ := teleports.Get(name)
		key := normalizeComponentLookup(name)
		if _, exists := components.items[key]; exists {
			return nil, fmt.Errorf("teleport component %q conflicts with a regular component", name)
		}
		components.items[key] = component
		components.names = append(components.names, name)
	}
	sort.Strings(components.names)
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		return nil, err
	}
	content, err := loadContent(systemContentDir, "/assets/content")
	if err != nil {
		return nil, err
	}
	base, err := loadBaseTemplate()
	if err != nil {
		return nil, err
	}
	globals, err := loadGlobalInserts(systemGlobalDir)
	if err != nil {
		return nil, err
	}
	baseCSS, err := loadDefaultCSS()
	if err != nil {
		return nil, err
	}
	loader := loadLoader(filepath.Join(systemTemplatesDir, "loader.gosh"))
	spaLoadingTemplate := loadSPALoadingTemplate(cfg.Render.SPALoadingTemplate, logger)
	layouts, err := LoadComponents(systemLayoutsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if layouts == nil {
		layouts = &Components{items: map[string]*Component{}}
	}
	ensureDefaultLayout(layouts, cfg.Render.DefaultLayout)
	tailwind := TailwindBuild{CSSByPage: map[string]string{}, Sources: map[string][]string{}}
	if prebuilt, ok := productionTailwindCSS(); ok {
		tailwind.CSSByPage = prebuilt
	} else {
		tailwind, err = BuildTailwind(pages, components, layouts, cfg)
		if err != nil {
			return nil, err
		}
	}
	scriptUsage := AnalyzeScriptUsage(pages, components)
	sharedSources := sharedLayoutScriptSources(layouts)
	for id, source := range sharedTeleportScriptSources(teleports, components) {
		sharedSources[id] = source
	}
	for id, source := range reusableComponentScriptSources(components, scriptUsage) {
		sharedSources[id] = source
	}
	entrySources := map[string]bool{}
	clientEntry, ok := productionClientEntry()
	if !ok {
		clientEntry, err = BuildClientEntry(sharedSources)
		if err != nil {
			clientEntry = embeddedRuntimeJS
		} else if clientEntrySupportsModuleRegistry(clientEntry) {
			entrySources = sourceIDSet(sharedSources)
		}
	} else if clientEntrySupportsModuleRegistry(clientEntry) {
		entrySources = sourceIDSet(sharedSources)
	}
	clientURL := "/_gosh/entry/" + hashText(string(clientEntry)) + ".js"
	vitalsURL := ""
	if cfg.Runtime.WebVitals {
		vitalsURL = "/_gosh/vitals/" + hashText(string(embeddedVitalsJS)) + ".js"
	}
	webSocketChunkURL := "/_gosh/websocket/" + hashText(string(embeddedWebSocketJS)) + ".js"
	commonCSS := systemBaseCSS + tailwind.CSSByPage["@shared"]
	unifiedCSS := buildUnifiedCSS(commonCSS, pages, components, layouts)
	app := &App{
		components:         components,
		teleports:          teleports,
		pages:              pages,
		layouts:            layouts,
		config:             cfg,
		renderer:           NewRenderer(components, loader),
		base:               base,
		baseShell:          documentShell{},
		globals:            globals,
		modules:            map[string]string{},
		chunks:             map[string]string{},
		styles:             map[string]string{},
		finalCSS:           map[string]string{},
		precompiledStyles:  map[string][]string{},
		scriptUsage:        scriptUsage,
		sourceChunks:       map[string]map[string]string{},
		tailwindCSS:        tailwind.CSSByPage,
		commonCSS:          commonCSS,
		unifiedCSS:         unifiedCSS,
		clientEntry:        clientEntry,
		entrySources:       entrySources,
		clientURL:          clientURL,
		vitalsURL:          vitalsURL,
		webSocketChunkURL:  webSocketChunkURL,
		spaLoadingTemplate: spaLoadingTemplate,
		routeCache:         newRenderCache(1024),
		componentDataCache: newRenderCache(2048),
		apiCache:           newHTTPResponseCache(512),
		runtimeState:       newRuntimeStateStore(),
		pagePlans:          map[string]ModulePlan{},
		content:            content,
		baseCSS:            baseCSS,
		tenants:            map[string]*tenantWeb{},
		responder:          standaloneWebResponder{},
	}
	app.baseShell, err = buildBaseShell(base)
	if err != nil {
		return nil, fmt.Errorf("compile base document shell: %w", err)
	}
	app.runtimeConfigHTML = app.runtimeConfigScript()
	app.browserWarningHTML = `<script>(function(){var w=window,d=document;if(typeof w.Promise==='function'&&typeof w.fetch==='function'&&typeof w.URL==='function'&&typeof w.AbortController==='function'&&typeof w.Map==='function'&&typeof w.Set==='function')return;var b=d.createElement('div');b.id='gosh-browser-warning';b.className='gosh-browser-warning';b.setAttribute('role','alert');b.appendChild(d.createTextNode('Your browser is outdated. This page is available, but interactive features are disabled. Please update your browser.'));d.body.insertBefore(b,d.body.firstChild);w._gosh=w._gosh||{};w._gosh.runtimeUnsupported=true}())</script>`
	app.skipLinkHTML = `<a href="#main" class="skip-link">Go to main content</a>`
	app.preloaderHTML = `<div id="gosh-preloader" class="preloader is-loading" role="status" aria-live="polite" aria-label="Loading" aria-busy="true"></div>`
	app.globalScriptsHTML = app.globals.Scripts + "\n" + `<script src="` + html.EscapeString(app.clientURL) + `" defer data-runtime-persistent></script>`
	if err := app.precompileStaticStyles(); err != nil {
		return nil, err
	}
	if err := app.precompileRouteClientChunks(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) precompileRouteClientChunks() error {
	if _, production := productionClientEntry(); production {
		chunks, routes, ok := productionRouteChunks()
		if !ok {
			return nil
		}
		for id, chunk := range chunks {
			a.chunks[id] = string(chunk)
		}
		for page, sources := range routes {
			if a.sourceChunks[page] == nil {
				a.sourceChunks[page] = map[string]string{}
			}
			for sourceID, chunkID := range sources {
				a.sourceChunks[page][sourceID] = "/_gosh/chunk/" + chunkID + ".js"
			}
		}
		return nil
	}
	if a.pages == nil {
		return nil
	}
	for _, page := range append(append([]*Page{}, a.pages.items...), a.pages.NotFound, a.pages.ErrorPage) {
		if page == nil || page.View == nil {
			continue
		}
		sources := routeClientSources(page, a.components, a.layouts, a.config.Render.DefaultLayout, a.entrySources, a.scriptUsage, a.config.CookieControl.Enabled)
		if len(sources) == 0 {
			continue
		}
		chunk, err := BuildRouteClientChunk(sources)
		if err != nil {
			return err
		}
		id := hashText(string(chunk))
		a.chunks[id] = string(chunk)
		url := "/_gosh/chunk/" + id + ".js"
		if a.sourceChunks[page.RelativePath] == nil {
			a.sourceChunks[page.RelativePath] = map[string]string{}
		}
		for sourceID := range sources {
			a.sourceChunks[page.RelativePath][sourceID] = url
		}
	}
	return nil
}

func routeClientSources(page *Page, components, layouts *Components, defaultLayout string, entrySources map[string]bool, scriptUsage map[string]int, cookieControlEnabled bool) map[string]string {
	sources := map[string]string{}
	seen := map[string]bool{}
	var visit func(*Component)
	var nodes func([]Node)
	visit = func(component *Component) {
		if component == nil || seen[component.Path] {
			return
		}
		if !cookieControlEnabled && disabledCookieGlobalComponent(component.Name) {
			return
		}
		seen[component.Path] = true
		for _, block := range component.Scripts {
			source := strings.TrimSpace(block.Content)
			id := hashText(source)
			if source != "" && !entrySources[id] && scriptUsage[id] <= 1 {
				sources[id] = source
			}
		}
		if component.PreparedSetupSource != "" {
			source := component.PreparedSetupSource
			id := hashText(source)
			if strings.TrimSpace(component.ScriptSetup.Content) != "" && !entrySources[id] && scriptUsage[id] <= 1 {
				sources[id] = source
			}
		}
		nodes(component.Template)
	}
	nodes = func(items []Node) {
		for _, node := range items {
			e, ok := node.(*ElementNode)
			if !ok {
				continue
			}
			if e.IsComponent {
				if component, ok := components.Get(e.Tag); ok {
					visit(component)
				}
			}
			nodes(e.Children)
		}
	}
	visit(page.View)
	layoutName := page.Layout
	if layoutName == "" {
		layoutName = defaultLayout
	}
	if layoutName != "" && layoutName != "none" && layouts != nil {
		if layout, ok := layouts.Get(layoutName); ok {
			visit(layout)
		}
	}
	return sources
}

func clientEntrySupportsModuleRegistry(entry []byte) bool {
	return bytes.Contains(entry, []byte("__GOSH_ENTRY_MODULES__"))
}

func hashText(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func (a *App) registerModule(source string) string {
	id := hashText(source)
	a.modules[id] = source
	return id
}

func (a *App) registerChunk(source string) string {
	id := hashText(source)
	a.chunks[id] = source
	return id
}

func setupScriptSource(source string) string {
	names := []string{"useRoot", "useElement", "onMounted", "onUnmounted", "useEvent", "useStore", "watchStore", "emit"}
	var b strings.Builder
	b.WriteString("export function mount(ctx){const __gosh=ctx.runtime.createComponentAPI(ctx);")
	declared := map[string]bool{}
	for _, match := range setupHelperDeclarationPattern.FindAllStringSubmatch(source, -1) {
		if len(match) > 1 {
			declared[match[1]] = true
		}
	}
	for _, name := range names {
		if declared[name] {
			continue
		}
		b.WriteString("const ")
		b.WriteString(name)
		b.WriteString("=__gosh.")
		b.WriteString(name)
		b.WriteByte(';')
	}
	b.WriteString(source)
	b.WriteByte('}')
	return b.String()
}

var setupHelperDeclarationPattern = regexp.MustCompile(`(?m)\b(?:const|let|var|function)\s+(useRoot|useElement|onMounted|onUnmounted|useEvent|useStore|watchStore|emit)\b`)

func (a *App) modulePlan(pageKey string, bindings []ScriptBinding, aggregateComponents bool) ModulePlan {
	if len(bindings) == 0 {
		return ModulePlan{}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if pageKey == "" {
		pageKey = "@fragment"
	}
	if a.sourceChunks[pageKey] == nil {
		a.sourceChunks[pageKey] = map[string]string{}
	}

	type pendingSource struct{ id, source string }
	pending := map[string]string{}
	for _, binding := range bindings {
		source := strings.TrimSpace(binding.Source)
		if source == "" || binding.Target == "" {
			continue
		}
		id := hashText(source)
		if !a.entrySources[id] && a.scriptUsage[id] <= 1 && a.sourceChunks[pageKey][id] == "" {
			pending[id] = source
		}
	}

	plan := ModulePlan{}
	seen := map[string]bool{}
	for _, binding := range bindings {
		source := strings.TrimSpace(binding.Source)
		if source == "" || binding.Target == "" {
			continue
		}
		id := hashText(source)
		key := id + "|" + binding.Target
		mb := ModuleBinding{Target: binding.Target, Owner: binding.Owner}

		if aggregateComponents && binding.Owner != "" && strings.HasPrefix(binding.Target, `[data-gosh-instance="`) {
			mb.Target = ""
			mb.Selector = `[data-gosh-component="` + binding.Owner + `"]:not([data-gosh-lazy="1"])`
			key = id + "|selector|" + mb.Selector
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		if a.entrySources[id] {
			mb.ID = id
			plan.Bindings = append(plan.Bindings, mb)
			continue
		}
		if a.scriptUsage[id] > 1 {
			a.modules[id] = source
			mb.Src = "/_gosh/module/" + id + ".js"
			plan.Bindings = append(plan.Bindings, mb)
			continue
		}
		mb.ID = id
		mb.Chunk = a.sourceChunks[pageKey][id]
		if mb.Chunk == "" {
			a.modules[id] = source
			mb.Src = "/_gosh/module/" + id + ".js"
		}
		plan.Bindings = append(plan.Bindings, mb)
	}
	return plan
}

func (p ModulePlan) Empty() bool { return len(p.Bindings) == 0 }

func (a *App) pageKeyForURL(raw string) string {
	path := raw
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		path = "/"
	}
	if page, _, _ := a.selectPage(path); page != nil {
		return page.RelativePath
	}
	return path
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func lookupDotted(data Props, key string) any {
	if key == "" {
		return nil
	}
	parts := strings.Split(key, ".")
	var cur any = map[string]any(data)
	for _, part := range parts {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[part]
		case Props:
			cur = v[part]
		default:
			return nil
		}
	}
	return cur
}

func applySEOParams(s string, params any) string {
	values := map[string]any{}
	switch p := params.(type) {
	case map[string]any:
		values = p
	case map[string]string:
		for k, v := range p {
			values[k] = v
		}
	}
	for k, v := range values {
		repl := stringify(v)
		s = strings.ReplaceAll(s, "{"+k+"}", repl)
		s = strings.ReplaceAll(s, ":"+k, repl)
	}
	return s
}

func resolveSEO(state logic.SEOState, data Props) logic.SEOState {
	seo := state
	if logic.SEOText(seo.Title) == "" && seo.TitleKey != "" {
		seo.Title = lookupDotted(data, seo.TitleKey)
	}
	if logic.SEOText(seo.Description) == "" && seo.DescriptionKey != "" {
		seo.Description = lookupDotted(data, seo.DescriptionKey)
	}
	if seo.Params != nil {
		seo.Title = applySEOParams(logic.SEOText(seo.Title), seo.Params)
		seo.Description = applySEOParams(logic.SEOText(seo.Description), seo.Params)
	}
	return seo
}

func seoHeadHTML(seo logic.SEOState) string {
	title := logic.SEOText(seo.Title)
	desc := logic.SEOText(seo.Description)
	var b strings.Builder
	meta := func(attr, name, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		b.WriteString(`<meta data-gosh-seo ` + attr + `="` + html.EscapeString(name) + `" content="` + html.EscapeString(content) + `">`)
	}
	meta("property", "og:type", "website")
	meta("property", "og:title", title)
	meta("name", "twitter:title", title)
	meta("property", "og:description", desc)
	meta("name", "twitter:description", desc)
	if seo.Image != "" {
		meta("name", "twitter:card", "summary_large_image")
		meta("property", "og:image", seo.Image)
		meta("name", "twitter:image", seo.Image)
	}
	if seo.NoIndexValue() {
		meta("name", "robots", "noindex,nofollow")
	}
	return b.String()
}

func valueString(data Props, key, fallback string) string {
	if v, ok := data[key]; ok {
		if s := stringify(v); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return fallback
}

func (a *App) registerStyle(css string) string {
	if strings.TrimSpace(css) == "" {
		return ""
	}
	css = minifyCSS(css)
	rawID := hashText(css)
	a.mu.Lock()
	processed, ok := a.finalCSS[rawID]
	a.mu.Unlock()
	if !ok {
		processed = css
		a.mu.Lock()
		if existing, exists := a.finalCSS[rawID]; exists {
			processed = existing
		} else {
			if a.finalCSS == nil {
				a.finalCSS = map[string]string{}
			}
			a.finalCSS[rawID] = processed
		}
		a.mu.Unlock()
	}
	id := hashText(processed)
	a.mu.Lock()
	if a.styles == nil {
		a.styles = map[string]string{}
	}
	a.styles[id] = processed
	a.mu.Unlock()
	return "/_gosh/style/" + id + ".css"
}

var cssWhitespacePattern = regexp.MustCompile(`\s*([{}:;,>])\s*`)
var cssCommentPattern = regexp.MustCompile(`/\*[\s\S]*?\*/`)

func minifyCSS(css string) string {
	css = cssCommentPattern.ReplaceAllString(css, "")
	css = strings.Join(strings.Fields(css), " ")
	css = cssWhitespacePattern.ReplaceAllString(css, "$1")
	css = strings.ReplaceAll(css, ";}", "}")
	return strings.TrimSpace(css)
}

func (a *App) precompileStaticStyles() error {
	if a.precompiledStyles == nil {
		a.precompiledStyles = map[string][]string{}
	}
	compile := func(css string) (string, error) {
		if strings.TrimSpace(css) == "" {
			return "", nil
		}
		rawID := hashText(css)
		processed, err := processFinalCSS(css)
		if err != nil {
			return "", err
		}
		processed = minifyCSS(processed)
		id := hashText(processed)
		a.finalCSS[rawID] = processed
		a.styles[id] = processed
		return "/_gosh/style/" + id + ".css", nil
	}
	if !a.config.Render.SplitCSS {
		url, err := compile(a.unifiedCSS)
		if err != nil {
			return fmt.Errorf("precompile application CSS: %w", err)
		}
		for _, page := range pageList(a.pages) {
			if url != "" {
				a.precompiledStyles[page.RelativePath] = []string{url}
			}
		}
		return nil
	}

	common := []string{}
	for _, chunk := range buildCSSChunks([]string{a.commonCSS}, true, a.config.Render.CSSMinChunkSize, a.config.Render.CSSMaxChunkSize) {
		url, err := compile(chunk)
		if err != nil {
			return fmt.Errorf("precompile common CSS: %w", err)
		}
		if url != "" {
			common = append(common, url)
		}
	}
	for _, page := range pageList(a.pages) {
		parts := a.staticCSSForPage(page)
		urls := append([]string(nil), common...)
		for _, chunk := range buildCSSChunks(parts, true, a.config.Render.CSSMinChunkSize, a.config.Render.CSSMaxChunkSize) {
			url, err := compile(chunk)
			if err != nil {
				return fmt.Errorf("precompile CSS for %s: %w", page.RelativePath, err)
			}
			if url != "" {
				urls = append(urls, url)
			}
		}
		a.precompiledStyles[page.RelativePath] = urls
	}
	return nil
}

func (a *App) staticCSSForPage(page *Page) []string {
	seen := map[string]bool{}
	parts := []string{}
	addViews := func(views []*Component) {
		for _, view := range views {
			if view == nil || seen[view.Path] {
				continue
			}
			seen[view.Path] = true
			if css := view.CSS(); strings.TrimSpace(css) != "" {
				parts = append(parts, css)
			}
		}
	}
	addViews(reachableViewsForPage(page, a.components, a.layouts, a.config.Render.DefaultLayout))
	if a.teleports != nil {
		for _, name := range a.teleports.Names() {
			if view, ok := a.teleports.Get(name); ok {
				addViews(reachableViewsForPage(&Page{View: view, Layout: "none"}, a.components, nil, ""))
			}
		}
	}
	return parts
}

func splitCSSUnits(css string) []string {
	var units []string
	start := 0
	depth := 0
	var quote byte
	comment := false
	for i := 0; i < len(css); i++ {
		c := css[i]
		if comment {
			if c == '*' && i+1 < len(css) && css[i+1] == '/' {
				comment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '/' && i+1 < len(css) && css[i+1] == '*' {
			comment = true
			i++
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				unit := strings.TrimSpace(css[start : i+1])
				if unit != "" {
					units = append(units, unit+"\n")
				}
				start = i + 1
			}
		case ';':
			if depth == 0 {
				unit := strings.TrimSpace(css[start : i+1])
				if unit != "" {
					units = append(units, unit+"\n")
				}
				start = i + 1
			}
		}
	}
	if tail := strings.TrimSpace(css[start:]); tail != "" {
		units = append(units, tail+"\n")
	}
	return units
}

func buildCSSChunks(parts []string, split bool, minSize, maxSize int) []string {
	var all strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		all.WriteString(part)
		if !strings.HasSuffix(part, "\n") {
			all.WriteByte('\n')
		}
	}
	full := all.String()
	if strings.TrimSpace(full) == "" {
		return nil
	}
	if !split {
		return []string{full}
	}
	if minSize <= 0 {
		minSize = 4096
	}
	if maxSize <= 0 {
		maxSize = 65536
	}
	if maxSize < minSize {
		maxSize = minSize
	}
	if len(full) <= maxSize {
		return []string{full}
	}

	var units []string
	for _, part := range parts {
		units = append(units, splitCSSUnits(part)...)
	}
	if len(units) == 0 {
		return []string{full}
	}
	var chunks [][]string
	var current []string
	currentSize := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, current)
		current = nil
		currentSize = 0
	}
	for _, unit := range units {
		if currentSize > 0 && currentSize+len(unit) > maxSize {
			flush()
		}
		current = append(current, unit)
		currentSize += len(unit)
	}
	flush()
	chunkSize := func(chunk []string) int {
		size := 0
		for _, unit := range chunk {
			size += len(unit)
		}
		return size
	}
	if len(chunks) > 1 && chunkSize(chunks[len(chunks)-1]) < minSize {
		previous := len(chunks) - 2
		last := len(chunks) - 1
		for len(chunks[previous]) > 1 && chunkSize(chunks[last]) < minSize {
			unitIndex := len(chunks[previous]) - 1
			unit := chunks[previous][unitIndex]
			if chunkSize(chunks[last])+len(unit) > maxSize || chunkSize(chunks[previous])-len(unit) < minSize {
				break
			}
			chunks[previous] = chunks[previous][:unitIndex]
			chunks[last] = append([]string{unit}, chunks[last]...)
		}
	}
	out := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, strings.Join(chunk, ""))
	}
	return out
}

func preloadTag(href, as string) string {
	if href == "" {
		return ""
	}
	return `<link rel="preload" href="` + html.EscapeString(href) + `" as="` + as + `" data-runtime-persistent>`
}

func (a *App) lazyCSSForPage(page *Page) []string {
	if page == nil || a.components == nil {
		return nil
	}
	var parts []string
	for _, view := range reachableViewsForPage(page, a.components, a.layouts, a.config.Render.DefaultLayout) {
		if css := view.CSS(); strings.TrimSpace(css) != "" {
			parts = append(parts, css)
		}
	}
	return parts
}

func buildUnifiedCSS(common string, pages *Pages, components, layouts *Components) string {
	seen := map[string]bool{}
	styles := []string{common}
	add := func(view *Component) {
		if view == nil || seen[view.Path] {
			return
		}
		seen[view.Path] = true
		if css := view.CSS(); strings.TrimSpace(css) != "" {
			styles = append(styles, css)
		}
	}
	if pages != nil {
		for _, page := range pageList(pages) {
			if page != nil {
				add(page.View)
			}
		}
	}
	for _, group := range []*Components{components, layouts} {
		if group == nil {
			continue
		}
		for _, name := range group.Names() {
			if view, ok := group.Get(name); ok {
				add(view)
			}
		}
	}
	return strings.Join(buildCSSChunks(styles, false, 0, 0), "")
}

func (a *App) assetLinks(page *Page, result RenderResult) (runtimeURL string, styleURLs []string) {
	if result.RuntimeURL != "" {
		return result.RuntimeURL, append([]string(nil), result.StyleURLs...)
	}
	runtimeURL = a.clientURL
	if page != nil {
		if urls, ok := a.precompiledStyles[page.RelativePath]; ok {
			return runtimeURL, append([]string(nil), urls...)
		}
	}
	parts := append([]string(nil), result.CSSParts...)
	if len(result.CSSParts) == 0 && strings.TrimSpace(result.CSS) != "" {
		parts = []string{result.CSS}
	}
	if !a.config.Render.SplitCSS {
		css := a.unifiedCSS
		if strings.TrimSpace(css) == "" {
			parts = append(parts, a.lazyCSSForPage(page)...)
			parts = append([]string{a.commonCSS}, parts...)
			css = strings.Join(buildCSSChunks(parts, false, 0, 0), "")
		}
		for _, chunk := range buildCSSChunks([]string{css}, false, 0, 0) {
			if u := a.registerStyle(chunk); u != "" {
				styleURLs = append(styleURLs, u)
			}
		}
		return
	}
	for _, chunk := range buildCSSChunks([]string{a.commonCSS}, true, a.config.Render.CSSMinChunkSize, a.config.Render.CSSMaxChunkSize) {
		if u := a.registerStyle(chunk); u != "" {
			styleURLs = append(styleURLs, u)
		}
	}
	for _, chunk := range buildCSSChunks(parts, true, a.config.Render.CSSMinChunkSize, a.config.Render.CSSMaxChunkSize) {
		if u := a.registerStyle(chunk); u != "" {
			styleURLs = append(styleURLs, u)
		}
	}
	return
}

func (a *App) prepareRenderAssets(page *Page, result *RenderResult) {
	if result == nil || result.RuntimeURL != "" {
		return
	}
	runtimeURL, styleURLs := a.assetLinks(page, *result)
	result.RuntimeURL = runtimeURL
	result.StyleURLs = styleURLs
}

func (a *App) sendEarlyHints(w http.ResponseWriter, r *http.Request, runtimeURL string, styleURLs []string) {
	if !a.config.Render.EarlyHints || requestWantsConnectionClose(r) || isSameOriginNavigation(r) {
		return
	}
	if strings.Contains(reflect.TypeOf(w).String(), "httptest.ResponseRecorder") {
		return
	}
	links := []string{}
	if a.config.Render.PreloadPageStyles {
		for _, styleURL := range styleURLs {
			if styleURL != "" {
				links = append(links, "<"+styleURL+">; rel=preload; as=style")
			}
		}
	}
	if a.config.Render.PreloadRuntime && runtimeURL != "" {
		links = append(links, "<"+runtimeURL+">; rel=preload; as=script")
	}
	if len(links) == 0 {
		return
	}
	for _, v := range links {
		w.Header().Add("Link", v)
	}
	w.WriteHeader(http.StatusEarlyHints)
	w.Header().Del("Link")
}

func requestWantsConnectionClose(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Close {
		return true
	}
	for _, value := range r.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "close") {
				return true
			}
		}
	}
	return false
}

func isSameOriginNavigation(r *http.Request) bool {
	if r == nil || r.Referer() == "" {
		return false
	}
	referer, err := url.Parse(r.Referer())
	return err == nil && referer.Host != "" && strings.EqualFold(referer.Host, r.Host)
}

func pageStyleLinks(styleURLs []string) string {
	if len(styleURLs) == 0 {
		return `<link id="gosh-page-styles" rel="stylesheet" disabled>`
	}
	var b strings.Builder
	for i, href := range styleURLs {
		b.WriteString(`<link rel="stylesheet" data-gosh-page-style="`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`" href="`)
		b.WriteString(html.EscapeString(href))
		b.WriteString(`">`)
	}
	return b.String()
}

func routePathFromResult(data Props) string {
	route, ok := data["route"].(map[string]any)
	if !ok {
		return ""
	}
	path, _ := route["path"].(string)
	return CanonicalURL(path)
}

func (a *App) alternateLanguageHead(page *Page, status int, data Props) string {
	if page == nil || status != http.StatusOK || len(a.config.Locales) == 0 || strings.HasPrefix(page.RelativePath, "@content/") {
		return ""
	}
	path := routePathFromResult(data)
	if path == "" || path == "/post" || strings.HasPrefix(path, "/post/") {
		return ""
	}
	defaultLocale := a.config.DefaultLocale
	if defaultLocale == "" {
		defaultLocale = a.config.Locales[0]
	}
	parts := splitURLPath(path)
	if len(parts) > 0 {
		for _, locale := range a.config.Locales {
			if parts[0] != locale {
				continue
			}
			if locale != defaultLocale && page.Route == path {
				return ""
			}
			if locale != defaultLocale {
				path = "/" + strings.Join(parts[1:], "/")
				if path == "" {
					path = "/"
				}
			}
			break
		}
	}
	var head strings.Builder
	writeLink := func(locale, href string) {
		head.WriteString(`<link rel="alternate" hreflang="`)
		head.WriteString(html.EscapeString(locale))
		head.WriteString(`" href="`)
		head.WriteString(html.EscapeString(href))
		head.WriteString(`">`)
	}
	for _, locale := range a.config.Locales {
		href := path
		if locale != defaultLocale {
			if path == "/" {
				href = "/" + locale
			} else {
				href = "/" + locale + path
			}
		}
		writeLink(locale, href)
	}
	writeLink("x-default", path)
	return head.String()
}

func storeStateScript(stores map[string]map[string]any) string {
	if len(stores) == 0 {
		return ""
	}
	payload, err := json.Marshal(map[string]any{"stores": stores})
	if err != nil {
		return ""
	}
	return `<script id="gosh-store-state" type="application/json" data-runtime-persistent>` + string(payload) + `</script>`
}

func pageRuntimeScript(page string, state string, plan ModulePlan) string {
	if strings.TrimSpace(state) == "" || plan.Empty() {
		return ""
	}
	payload, err := json.Marshal(struct {
		State string `json:"state"`
	}{State: state})
	if err != nil {
		return ""
	}
	return `<script id="gosh-page-runtime" type="application/json" data-runtime-persistent>` + string(payload) + `</script>`
}

func (a *App) runtimeConfigScript() string {
	config := map[string]any{
		"webSocketChunkURL":     a.webSocketChunkURL,
		"prefetchDelay":         a.config.Runtime.PrefetchDelay,
		"prefetchMaxConcurrent": a.config.Runtime.PrefetchMaxConcurrent,
		"prefetchOnHover":       a.config.Runtime.PrefetchOnHover,
		"viewTransitions":       a.config.Runtime.ViewTransitions,
	}
	if a.config.Runtime.WebVitals {
		config["webVitals"] = true
		config["webVitalsURL"] = a.vitalsURL
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return ""
	}
	return `<script id="gosh-runtime-config" type="application/json" data-runtime-persistent>` + string(payload) + `</script>`
}

func (a *App) renderBase(result RenderResult, page *Page, status int, nonce, stateToken string, publicStatic bool) ([]byte, error) {
	if publicStatic {
		result.HTML = removeRuntimeStateAttributes(result.HTML)
		result.TeleportHTML = removeRuntimeStateAttributes(result.TeleportHTML)
		result.Head = staticScriptElementPattern.ReplaceAllString(result.Head, "")
	}
	var inlineCSS string
	result.HTML, inlineCSS = nonceStylesForHTML(result.HTML)
	var teleportCSS string
	result.TeleportHTML, teleportCSS = nonceStylesForHTML(result.TeleportHTML)
	inlineCSS += teleportCSS
	plan := ModulePlan{}
	if !publicStatic {
		plan = result.RuntimePlan
		if plan.Empty() && len(result.Scripts) > 0 {
			plan = a.modulePlan(page.RelativePath, result.Scripts, true)
		}
		if stateToken != "" && stateToken != cachedDocumentRootMarker {
			a.rememberPagePlan(stateToken, plan)
		}
	}
	var content strings.Builder
	if !publicStatic {
		content.WriteString(a.browserWarningHTML)
	}
	content.WriteString(a.skipLinkHTML)
	if !publicStatic {
		content.WriteString(a.spaLoadingTemplate)
		content.WriteString(a.runtimeConfigHTML)
		content.WriteString(storeStateScript(result.Stores))
		content.WriteString(pageRuntimeScript(page.RelativePath, stateToken, plan))
	}
	content.WriteString(`<div id="app"`)
	if publicStatic {
		content.WriteString(` data-gosh-public-static="1"`)
	}
	content.WriteByte('>')
	content.WriteString(result.HTML)
	content.WriteString(`</div>`)
	if result.TeleportHTML != "" {
		content.WriteString(`<div id="gosh-teleports" aria-live="off">`)
		content.WriteString(result.TeleportHTML)
		content.WriteString(`</div>`)
	}
	if !publicStatic {
		content.WriteString(a.preloaderHTML)
	}

	runtimeURL, styleURLs := a.assetLinks(page, result)
	pageStyles := pageStyleLinks(styleURLs)
	preloads := ""
	if !publicStatic && !a.config.Render.EarlyHints {
		if a.config.Render.PreloadRuntime {
			preloads += preloadTag(runtimeURL, "script")
		}
	}

	seo := result.SEO
	globalStyles := `<style>#gosh-teleports{position:fixed;inset:0;overflow:clip;z-index:9998;pointer-events:none}#gosh-teleports>gosh-component{pointer-events:auto}#gosh-teleports .gosh-system-teleport-loader{visibility:hidden}.skip-link{position:fixed;z-index:10001;top:0;left:0;padding:.75rem 1rem;background:#fff;color:#111;transform:translateY(-120%)}.skip-link:focus{transform:translateY(0);outline:3px solid #2563eb;outline-offset:2px}.gosh-visually-hidden{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}.gosh-browser-warning{position:sticky;top:0;z-index:10002;margin:0;padding:.75rem 1rem;background:#7f1d1d;color:#fff;font:600 1rem/1.4 system-ui,sans-serif;text-align:center}</style>`
	if inlineCSS != "" {
		globalStyles += `<style nonce="` + html.EscapeString(nonce) + `" data-gosh-inline-styles>` + inlineCSS + `</style>`
	}
	globalHead := a.globals.Head
	pageHead := result.Head + seoHeadHTML(seo) + a.alternateLanguageHead(page, status, result.Data)
	globalScripts := ""
	if !publicStatic {
		globalHead += "\n" + `<script data-runtime-persistent>window._gosh=window._gosh||{};window._gosh.domReady=window._gosh.domReady||new Promise(function(resolve){if(document.readyState!=="loading")resolve();else document.addEventListener("DOMContentLoaded",resolve,{once:true});});</script>`
		pageHead = `<meta name="gosh-csp-nonce" content="` + html.EscapeString(nonce) + `" data-runtime-persistent>` + preloads + pageHead
		globalScripts = a.globalScriptsHTML
		if runtimeURL != a.clientURL {
			globalScripts = a.globals.Scripts + "\n" + `<script src="` + html.EscapeString(runtimeURL) + `" defer data-runtime-persistent></script>`
		}
	}
	data := BasePageData{
		Variables: map[string]any{
			"lang":        valueString(result.Data, "lang", firstNonEmpty(a.config.DefaultLocale, "en")),
			"title":       firstNonEmpty(logic.SEOText(seo.Title), valueString(result.Data, "title", "Cool web site powered by MyelophOne GoServer")),
			"description": firstNonEmpty(logic.SEOText(seo.Description), valueString(result.Data, "description", "This cool site was created using MyelophOne GoServer")),
		},
		GlobalHead:    stdtemplate.HTML(globalHead),
		PageHead:      stdtemplate.HTML(pageHead),
		GlobalStyles:  stdtemplate.HTML(globalStyles),
		PageStyles:    stdtemplate.HTML(pageStyles),
		GlobalScripts: stdtemplate.HTML(globalScripts),
		Content:       stdtemplate.HTML(content.String()),
	}
	var rawBody []byte
	if !publicStatic && len(a.baseShell.parts) > 0 {
		rawBody = materializeBaseShell(a.baseShell, data)
	} else {
		var buf bytes.Buffer
		if err := a.base.ExecuteTemplate(&buf, "base", data); err != nil {
			return nil, err
		}
		rawBody = buf.Bytes()
	}
	if publicStatic {
		rawBody = staticScriptElementPattern.ReplaceAll(rawBody, nil)
	}
	return finalizeTrustedHTML(rawBody, nonce), nil
}

func (a *App) renderPage(r *http.Request, page *Page, params map[string]any) (RenderResult, error) {
	if err := logic.CallHook(logic.HookServerRenderBefore, &logic.HookPayload{App: logic.UseApp(), Key: page.RelativePath, Data: params}); err != nil {
		return RenderResult{}, err
	}
	publicParams := publicRouteParams(params)
	ctx := logicContextWithPublicParams(r, params, publicParams)
	a.localizeContext(ctx, r)
	ctx.SetSEODefaults(a.config.SEO)
	ctx.SetFullscreenPreloaderDefaults(a.config.Preloader)
	props := requestPropsWithPublicParams(r, params, publicParams)
	if _, exists := props["lang"]; !exists && a.config.DefaultLocale != "" {
		props["lang"] = a.config.DefaultLocale
	}
	for k, v := range a.config.Values {
		if _, exists := props[k]; !exists {
			props[k] = v
		}
	}
	props["config"] = a.config.Values
	renderer, layouts, teleports, err := a.renderingForRequest(r)
	if err != nil {
		return RenderResult{}, err
	}
	res, err := renderer.RenderComponentContext(page.View, props, ctx)
	if err != nil {
		return res, err
	}
	applyFullscreenPreloaderData(&res, ctx)
	layoutName := page.Layout
	if layoutName == "" {
		layoutName = a.config.Render.DefaultLayout
	}
	if layoutName != "none" && layoutName != "" && layouts != nil {
		if layout, ok := layouts.Get(layoutName); ok {
			res, err = renderer.RenderLayout(layout, res, ctx)
			if err != nil {
				return res, err
			}
		}
	}
	res.SEO = resolveSEO(ctx.SEO(), res.Data)
	if err := appendTeleports(&res, ctx, renderer, teleports); err != nil {
		return res, err
	}
	a.addTailwind(page, &res)
	a.addBaseCSS(&res)
	if err := logic.CallHook(logic.HookServerRenderAfter, &logic.HookPayload{App: logic.UseApp(), Key: page.RelativePath, Data: &res}); err != nil {
		return res, err
	}
	a.prepareRenderAssets(page, &res)
	res.RuntimePlan = a.cachedModulePlan(tenantIDFromRequest(r)+"|"+page.RelativePath, page.RelativePath, res.Scripts, true)
	return res, nil
}

func (a *App) cachedModulePlan(cacheKey, pageKey string, bindings []ScriptBinding, aggregateComponents bool) ModulePlan {
	if len(bindings) == 0 {
		return ModulePlan{}
	}
	cacheKey = cacheKey
	if aggregateComponents {
		cacheKey += "|aggregate"
	}
	if value, ok := a.modulePlans.Load(cacheKey); ok {
		return value.(ModulePlan)
	}
	plan := a.modulePlan(pageKey, bindings, aggregateComponents)
	actual, loaded := a.modulePlans.LoadOrStore(cacheKey, plan)
	if loaded {
		return actual.(ModulePlan)
	}
	return plan
}

func (p *Pages) ByRelative(path string) (*Page, bool) {
	path = filepath.ToSlash(path)
	for _, page := range p.items {
		if page.RelativePath == path {
			return page, true
		}
	}
	if p.NotFound != nil && p.NotFound.RelativePath == path {
		return p.NotFound, true
	}
	if p.ErrorPage != nil && p.ErrorPage.RelativePath == path {
		return p.ErrorPage, true
	}
	return nil, false
}

func selectPageFrom(pages *Pages, cfg logic.RuntimeConfig, path string, includeNotFound bool) (*Page, map[string]any, int) {
	locale := ""
	if len(cfg.Locales) > 0 {
		parts := splitURLPath(path)
		if len(parts) > 0 {
			for _, candidate := range cfg.Locales {
				if candidate == parts[0] && candidate != cfg.DefaultLocale {
					if page, params, ok := pages.MatchStatic(path); ok {
						return page, params, http.StatusOK
					}
					locale = candidate
					path = "/" + strings.Join(parts[1:], "/")
					if path == "/" {
						path = "/"
					}
					break
				}
			}
		}
	}
	if p, params, ok := pages.Match(path); ok {
		if locale != "" {
			params["locale"] = locale
		}
		return p, params, http.StatusOK
	}
	if includeNotFound && pages.NotFound != nil {
		params := map[string]any{}
		if locale != "" {
			params["locale"] = locale
		}
		return pages.NotFound, params, http.StatusNotFound
	}
	return nil, nil, http.StatusNotFound
}

func (a *App) selectPage(path string) (*Page, map[string]any, int) {
	return selectPageFrom(a.pages, a.config, path, true)
}

func safeTenantID(id string) bool {
	return id != "" && id != "." && filepath.Base(id) == id && !strings.ContainsAny(id, `/\\`)
}

func tenantIDFromRequest(r *http.Request) string {
	if tenant := GetTenant(r); tenant != nil && safeTenantID(tenant.ID) {
		return tenant.ID
	}
	return ""
}

func (a *App) tenantWebFor(r *http.Request) (*tenantWeb, error) {
	id := tenantIDFromRequest(r)
	if id == "" {
		return nil, nil
	}
	a.tenantMu.RLock()
	loaded, ok := a.tenants[id]
	a.tenantMu.RUnlock()
	if ok {
		return loaded, nil
	}
	value, err, _ := a.tenantFlights.Do(id, func() (any, error) {
		a.tenantMu.RLock()
		loaded, ok := a.tenants[id]
		a.tenantMu.RUnlock()
		if ok {
			return loaded, nil
		}
		root := filepath.Join(tenantRootDir, id)
		loaded = &tenantWeb{content: cloneContent(a.content)}
		pagesRoot := filepath.Join(root, "pages")
		if _, readErr := sourceReadDir(pagesRoot); readErr == nil {
			pages, loadErr := LoadPages(pagesRoot)
			if loadErr != nil {
				return nil, fmt.Errorf("load tenant %q pages: %w", id, loadErr)
			}
			loaded.pages = pages
		} else if !os.IsNotExist(readErr) {
			return nil, fmt.Errorf("read tenant %q pages: %w", id, readErr)
		}
		content, loadErr := loadContent(filepath.Join(root, "content"), "/assets/tenants/"+id+"/content")
		if loadErr != nil {
			return nil, fmt.Errorf("load tenant %q content: %w", id, loadErr)
		}
		for key, page := range content {
			loaded.content[key] = page
		}

		components, loadErr := loadTenantComponents(filepath.Join(root, "components"), a.components)
		if loadErr != nil {
			return nil, fmt.Errorf("load tenant %q components: %w", id, loadErr)
		}
		layouts, loadErr := loadTenantComponents(filepath.Join(root, "layouts"), a.layouts)
		if loadErr != nil {
			return nil, fmt.Errorf("load tenant %q layouts: %w", id, loadErr)
		}
		teleports, loadErr := loadTenantComponents(filepath.Join(root, "teleport"), a.teleports)
		if loadErr != nil {
			return nil, fmt.Errorf("load tenant %q teleports: %w", id, loadErr)
		}
		for _, name := range teleports.Names() {
			component, _ := teleports.Get(name)
			components.items[normalizeComponentLookup(name)] = component
		}
		components.names = componentNames(components.items)
		loaded.components = components
		loaded.layouts = layouts
		loaded.teleports = teleports
		loaded.renderer = NewRenderer(components, a.renderer.loader)
		loaded.renderer.logger = a.renderer.logger
		a.tenantMu.Lock()
		a.tenants[id] = loaded
		a.tenantMu.Unlock()
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*tenantWeb), nil
}

func cloneContent(source map[string]contentPage) map[string]contentPage {
	copy := make(map[string]contentPage, len(source))
	for key, page := range source {
		copy[key] = page
	}
	return copy
}

func componentNames(items map[string]*Component) []string {
	names := make([]string, 0, len(items))
	for _, component := range items {
		names = append(names, component.Name)
	}
	sort.Strings(names)
	return names
}

func ensureDefaultLayout(layouts *Components, name string) {
	if layouts == nil || name == "" || name == "none" {
		return
	}
	key := normalizeComponentLookup(name)
	if _, exists := layouts.items[key]; exists {
		return
	}
	layouts.items[key] = &Component{
		Name:         name,
		Path:         "<system default layout>",
		RelativePath: "<system default layout>",
		Template:     []Node{&ElementNode{Tag: "slot", SelfClosing: true}},
	}
	layouts.names = componentNames(layouts.items)
}

func loadTenantComponents(root string, base *Components) (*Components, error) {
	items := make(map[string]*Component)
	if base != nil {
		for key, component := range base.items {
			items[key] = component
		}
	}
	if _, err := sourceReadDir(root); err != nil {
		if os.IsNotExist(err) {
			return &Components{items: items, names: componentNames(items)}, nil
		}
		return nil, err
	}
	override, err := LoadComponents(root)
	if err != nil {
		return nil, err
	}
	for key, component := range override.items {
		items[key] = component
	}
	return &Components{items: items, names: componentNames(items)}, nil
}

func (a *App) renderingForRequest(r *http.Request) (*Renderer, *Components, *Components, error) {
	tenant, err := a.tenantWebFor(r)
	if err != nil {
		return nil, nil, nil, err
	}
	if tenant != nil {
		return tenant.renderer, tenant.layouts, tenant.teleports, nil
	}
	return a.renderer, a.layouts, a.teleports, nil
}

func (a *App) selectTenantPage(r *http.Request) (*Page, map[string]any, int, error) {
	tenant, err := a.tenantWebFor(r)
	if err != nil {
		return nil, nil, 0, err
	}
	if tenant != nil && tenant.pages != nil {
		if page, params, status := selectPageFrom(tenant.pages, a.config, r.URL.Path, false); page != nil {
			return page, params, status, nil
		}
	}
	page, params, status := a.selectPage(r.URL.Path)
	return page, params, status, nil
}

func (a *App) renderError(r *http.Request, sourceErr error) (RenderResult, *Page, error) {
	if a.pages.ErrorPage == nil {
		return RenderResult{}, nil, sourceErr
	}
	props := requestProps(r, map[string]any{})
	props["error"] = sourceErr.Error()
	props["statusCode"] = logic.ErrorStatus(sourceErr)
	var httpErr *logic.HTTPError
	if errors.As(sourceErr, &httpErr) {
		props["errorData"] = httpErr.Data
	}
	ctx := logicContext(r, map[string]any{})
	a.localizeContext(ctx, r)
	ctx.SetSEODefaults(a.config.SEO)
	ctx.SetFullscreenPreloaderDefaults(a.config.Preloader)
	for k, v := range a.config.Values {
		if _, ok := props[k]; !ok {
			props[k] = v
		}
	}
	props["config"] = a.config.Values
	res, err := a.renderer.RenderComponentContext(a.pages.ErrorPage.View, props, ctx)
	if err == nil {
		applyFullscreenPreloaderData(&res, ctx)
		layoutName := a.pages.ErrorPage.Layout
		if layoutName == "" {
			layoutName = a.config.Render.DefaultLayout
		}
		if layoutName != "" && layoutName != "none" {
			if layout, ok := a.layouts.Get(layoutName); ok {
				res, err = a.renderer.RenderLayout(layout, res, ctx)
			}
		}
	}
	if err == nil {
		res.SEO = resolveSEO(ctx.SEO(), res.Data)
		a.addTailwind(a.pages.ErrorPage, &res)
		a.addBaseCSS(&res)
	}
	return res, a.pages.ErrorPage, err
}

func isRuntimeRequest(r *http.Request) bool {
	return r.Header.Get("X-Runtime") == "1" &&
		r.Header.Get("X-GOSH-Runtime") == "navigate" &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/x-ndjson") &&
		r.Header.Get("Sec-Fetch-Dest") != "document"
}

type stream struct {
	enc     *json.Encoder
	flusher http.Flusher
}

func newStream(w http.ResponseWriter) *stream {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")
	f, _ := w.(http.Flusher)
	return &stream{enc: json.NewEncoder(w), flusher: f}
}
func (s *stream) Send(frame Frame) {
	if s.enc.Encode(frame) == nil && s.flusher != nil {
		s.flusher.Flush()
	}
}

func (a *App) styleLinkFrame(css string) Frame {
	href := a.registerStyle(css)
	return Frame{"v": 1, "type": "style-link", "href": href}
}

func (a *App) streamPage(w http.ResponseWriter, r *http.Request, page *Page, params map[string]any, status int, result RenderResult) {
	stateToken, err := a.bindRuntimeState(w, r, &result)
	if err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	ttl := 0
	if rule, ok := a.routeRule(r.URL.Path); ok {
		ttl, _ = routeTTL(rule)
	}
	if ttl > 0 {
		w.Header().Set("Cache-Control", "private, no-cache")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.Header().Set("X-Runtime-Cache-TTL", strconv.Itoa(ttl))
	s := newStream(w)
	s.Send(Frame{"v": 1, "type": "begin", "scope": "page", "url": r.URL.RequestURI(), "target": "#app", "clear": false, "history": "push", "status": status})
	if len(result.CacheTags) > 0 {
		s.Send(Frame{"v": 1, "type": "cache-tags", "tags": result.CacheTags})
	}
	seo := result.SEO
	title := firstNonEmpty(logic.SEOText(seo.Title), valueString(result.Data, "title", ""))
	desc := firstNonEmpty(logic.SEOText(seo.Description), valueString(result.Data, "description", ""))
	lang := valueString(result.Data, "lang", firstNonEmpty(a.config.DefaultLocale, "en"))
	s.Send(Frame{"v": 1, "type": "seo", "title": title, "description": desc, "image": seo.Image, "noIndex": seo.NoIndexValue(), "lang": lang})
	if len(result.Stores) > 0 {
		s.Send(Frame{"v": 1, "type": "store-state", "stores": result.Stores})
	}
	var inlineCSS string
	result.HTML, inlineCSS = nonceStylesForHTML(result.HTML)
	if inlineCSS != "" {
		s.Send(Frame{"v": 1, "type": "style", "id": "inline-" + hashText(inlineCSS), "css": inlineCSS})
	}
	s.Send(Frame{"v": 1, "type": "html", "target": "#app", "mode": "inner", "html": result.HTML})
	_, styleURLs := a.assetLinks(page, result)
	s.Send(Frame{"v": 1, "type": "page-styles", "hrefs": styleURLs})
	plan := a.modulePlan(page.RelativePath, result.Scripts, true)
	s.Send(Frame{"v": 1, "type": "page-runtime", "page": page.RelativePath, "state": stateToken, "modules": plan})
	if !plan.Empty() {
		s.Send(Frame{"v": 1, "type": "modules", "scope": "page", "plan": plan, "data": map[string]any{"path": r.URL.Path}})
	}
	s.Send(Frame{"v": 1, "type": "end", "target": "#app", "scroll": "top"})
}

func routePatternMatch(pattern, path string) bool {
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") && len(pattern) > 2 {
		return strings.Contains(path, strings.Trim(pattern, "*"))
	}
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		base := strings.TrimSuffix(pattern, "**")
		return strings.HasPrefix(path, strings.TrimSuffix(base, "/"))
	}
	if strings.Contains(pattern, "*") {
		pp := strings.Split(strings.Trim(pattern, "/"), "/")
		sp := strings.Split(strings.Trim(path, "/"), "/")
		if len(pp) != len(sp) {
			return false
		}
		for i := range pp {
			if pp[i] != "*" && pp[i] != sp[i] {
				return false
			}
		}
		return true
	}
	return false
}

func routeRuleExcluded(rule logic.RouteRule, path string) bool {
	for _, pattern := range rule.Exclude {
		if pattern != "" && routePatternMatch(pattern, path) {
			return true
		}
	}
	return false
}

func (a *App) routeRule(path string) (logic.RouteRule, bool) {
	best := ""
	var rule logic.RouteRule
	for pattern, candidate := range a.config.RouteRules {
		if routePatternMatch(pattern, path) && !routeRuleExcluded(candidate, path) && len(pattern) > len(best) {
			best = pattern
			rule = candidate
		}
	}
	return rule, best != ""
}

func routeTTL(rule logic.RouteRule) (ttl, swr int) {
	swr = rule.SWR
	if rule.Cache != nil {
		ttl = rule.Cache.MaxAge
	}
	if ttl <= 0 && swr > 0 {
		ttl = swr
	}
	return
}

func cacheableRequest(r *http.Request) bool {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
		return false
	}
	for _, cookie := range r.Cookies() {
		if cookie.Name != runtimeStateCookie {
			return false
		}
	}
	return true
}

func cacheablePageRequest(r *http.Request, publicStatic bool) bool {
	if publicStatic {
		return r.Method == http.MethodGet && r.Header.Get("Authorization") == ""
	}
	return cacheableRequest(r)
}

func publicCacheControl(rule logic.RouteRule) string {
	ttl, swr := routeTTL(rule)
	parts := []string{"public", "max-age=" + strconv.Itoa(max(0, ttl)), "s-maxage=" + strconv.Itoa(max(0, ttl))}
	if swr > 0 {
		parts = append(parts, "stale-while-revalidate="+strconv.Itoa(swr))
	}
	return strings.Join(parts, ", ")
}

type renderFlightResult struct {
	result RenderResult
	err    error
}

type sharedRenderCacheEntry struct {
	Result      RenderResult      `json:"result"`
	Stored      time.Time         `json:"stored"`
	TagVersions map[string]string `json:"tagVersions,omitempty"`
}

func webRouteCacheKey(r *http.Request) string {
	return "gosh:web:route:v1:" + tenantIDFromRequest(r) + ":" + r.URL.RequestURI()
}

func webTagVersionKey(tag string) string {
	return "gosh:web:tag:v1:" + hashText(tag)
}

func cacheBytes(value any) ([]byte, bool) {
	switch v := value.(type) {
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	default:
		return nil, false
	}
}

func (a *App) webCacheTagVersions(ctx context.Context, tags []string) map[string]string {
	versions := map[string]string{}
	if a.webCache == nil {
		return versions
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if raw, ok := a.webCache.Get(ctx, webTagVersionKey(tag)); ok {
			if value, ok := cacheBytes(raw); ok {
				versions[tag] = string(value)
				continue
			}
		}
		versions[tag] = "0"
		_ = a.webCache.Set(ctx, webTagVersionKey(tag), "0", 365*24*time.Hour)
	}
	return versions
}

func (a *App) sharedRenderGet(ctx context.Context, key string, ttl, swr int) (RenderResult, string, bool) {
	if a.webCache == nil {
		return RenderResult{}, "", false
	}
	raw, ok := a.webCache.Get(ctx, key)
	if !ok {
		return RenderResult{}, "", false
	}
	data, ok := cacheBytes(raw)
	if !ok {
		return RenderResult{}, "", false
	}
	var entry sharedRenderCacheEntry
	if json.Unmarshal(data, &entry) != nil || entry.Stored.IsZero() {
		return RenderResult{}, "", false
	}
	for tag, version := range entry.TagVersions {
		if a.webCacheTagVersions(ctx, []string{tag})[tag] != version {
			return RenderResult{}, "", false
		}
	}
	age := time.Since(entry.Stored)
	if age < time.Duration(ttl)*time.Second {
		return entry.Result, "shared-hit", true
	}
	if swr > 0 && age < time.Duration(ttl+swr)*time.Second {
		return entry.Result, "shared-stale", true
	}
	return RenderResult{}, "", false
}

func (a *App) sharedRenderSet(ctx context.Context, key string, result RenderResult, ttl, swr int) {
	if a.webCache == nil {
		return
	}
	entry := sharedRenderCacheEntry{Result: result, Stored: time.Now(), TagVersions: a.webCacheTagVersions(ctx, result.CacheTags)}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = a.webCache.Set(ctx, key, data, time.Duration(ttl+max(0, swr))*time.Second)
}

func (a *App) invalidateSharedRenderTags(ctx context.Context, tags []string) {
	if a.webCache == nil {
		return
	}
	for _, tag := range tags {
		if tag != "" {
			_ = a.webCache.Set(ctx, webTagVersionKey(tag), strconv.FormatInt(time.Now().UnixNano(), 36), 365*24*time.Hour)
		}
	}
}

func (a *App) setRouteCache(ctx context.Context, key string, result RenderResult) {
	a.routeCache.setWithTagVersions(key, result, a.webCacheTagVersions(ctx, result.CacheTags))
}

func routeRenderCacheKey(r *http.Request) string {
	return tenantIDFromRequest(r) + "|" + r.URL.RequestURI()
}

func (a *App) localRenderCacheValid(ctx context.Context, entry cacheEntry) bool {
	if len(entry.TagVersions) == 0 || a.webCache == nil {
		return true
	}
	for tag, version := range entry.TagVersions {
		if a.webCacheTagVersions(ctx, []string{tag})[tag] != version {
			return false
		}
	}
	return true
}

func (a *App) renderPageCached(r *http.Request, page *Page, params map[string]any) (RenderResult, string, error) {
	rule, ok := a.routeRule(r.URL.Path)
	if !ok || !cacheablePageRequest(r, rule.PublicStatic) {
		res, err := a.renderPage(r, page, params)
		return res, "bypass", err
	}
	ttl, swr := routeTTL(rule)
	if ttl <= 0 {
		res, err := a.renderPage(r, page, params)
		return res, "bypass", err
	}
	key := routeRenderCacheKey(r)
	sharedKey := webRouteCacheKey(r)
	if ent, found := a.routeCache.get(key); found && a.localRenderCacheValid(r.Context(), ent) {
		age := time.Since(ent.Stored)
		if age < time.Duration(ttl)*time.Second {
			_ = logic.CallHook(logic.HookServerCacheHit, &logic.HookPayload{App: logic.UseApp(), Key: key, Data: "route"})
			return ent.Result, "hit", nil
		}
		if swr > 0 && age < time.Duration(ttl+swr)*time.Second {
			if a.routeCache.beginRevalidate(key) {
				clone := r.Clone(context.WithoutCancel(r.Context()))
				paramsCopy := map[string]any{}
				for k, v := range params {
					paramsCopy[k] = v
				}
				task := logic.BindBackgroundApp(func() {
					defer a.routeCache.endRevalidate(key)
					if res, err := a.renderPage(clone, page, paramsCopy); err == nil {
						a.setRouteCache(context.Background(), key, res)
						a.sharedRenderSet(context.Background(), sharedKey, res, ttl, swr)
					}
				})
				go task()
			}
			_ = logic.CallHook(logic.HookServerCacheStale, &logic.HookPayload{App: logic.UseApp(), Key: key, Data: "route"})
			return ent.Result, "stale", nil
		}
	}
	if res, state, found := a.sharedRenderGet(r.Context(), sharedKey, ttl, swr); found {
		a.setRouteCache(r.Context(), key, res)
		if state == "shared-stale" && a.routeCache.beginRevalidate(key) {
			clone := r.Clone(context.WithoutCancel(r.Context()))
			paramsCopy := map[string]any{}
			for k, v := range params {
				paramsCopy[k] = v
			}
			go logic.BindBackgroundApp(func() {
				defer a.routeCache.endRevalidate(key)
				if fresh, err := a.renderPage(clone, page, paramsCopy); err == nil {
					a.setRouteCache(context.Background(), key, fresh)
					a.sharedRenderSet(context.Background(), sharedKey, fresh, ttl, swr)
				}
			})()
		}
		return res, state, nil
	}
	value, err, _ := a.routeCache.flights.Do(key, func() (any, error) {
		if ent, found := a.routeCache.get(key); found && a.localRenderCacheValid(r.Context(), ent) && time.Since(ent.Stored) < time.Duration(ttl)*time.Second {
			return renderFlightResult{result: ent.Result}, nil
		}
		_ = logic.CallHook(logic.HookServerCacheMiss, &logic.HookPayload{App: logic.UseApp(), Key: key, Data: "route"})
		res, renderErr := a.renderPage(r, page, params)
		if renderErr == nil {
			a.setRouteCache(r.Context(), key, res)
			a.sharedRenderSet(r.Context(), sharedKey, res, ttl, swr)
		}
		return renderFlightResult{result: res, err: renderErr}, nil
	})
	if err != nil {
		return RenderResult{}, "miss", err
	}
	flight := value.(renderFlightResult)
	return flight.result, "miss", flight.err
}

func (a *App) pageHandler(w http.ResponseWriter, r *http.Request) {
	requestStarted := time.Now()
	if len(r.URL.Path) > 1 && strings.HasSuffix(r.URL.Path, "/") {
		location := CanonicalURL(r.URL.Path)
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, http.StatusMovedPermanently)
		return
	}
	if r.Method != http.MethodGet {
		a.renderHTTPError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Header.Get("X-GOSH-Prefetch") == "1" {
		rule, ok := a.routeRule(r.URL.Path)
		ttl, _ := routeTTL(rule)
		if !ok || ttl <= 0 || !cacheablePageRequest(r, rule.PublicStatic) {
			w.Header().Set("X-GOSH-Prefetchable", "0")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("X-GOSH-Prefetchable", "1")
	}
	if strings.HasPrefix(r.URL.Path, "/post/") {
		a.contentHandler(w, r)
		return
	}
	page, params, status, selectErr := a.selectTenantPage(r)
	if selectErr != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, selectErr.Error())
		return
	}
	if page == nil {
		if a.publicFileHandler != nil && HasPublicFile(r.URL.Path) {
			a.publicFileHandler(w, r)
			return
		}
		a.renderHTTPError(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
		return
	}
	rule, hasRouteRule := a.routeRule(r.URL.Path)
	publicStatic := hasRouteRule && rule.PublicStatic
	result, cacheState, err := a.renderPageCached(r, page, params)
	renderedAt := time.Now()
	if err != nil {
		logic.SetError(err)
		_ = logic.CallHook(logic.HookServerError, &logic.HookPayload{App: logic.UseApp(), Error: err})
		a.renderer.logError("render %s: %v", r.URL.Path, err)
		if er, ep, e2 := a.renderError(r, err); e2 == nil && ep != nil {
			if a.errorRenderer != nil {
				a.renderHTTPError(w, r, logic.ErrorStatus(err), err.Error())
				return
			}
			result, page, status = er, ep, logic.ErrorStatus(err)
		} else {
			a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.Header().Set("X-MyelophOne-Cache", cacheState)
	if isSiteSearchRequest(r) {
		writeSiteSearchDocument(w, status, r, result)
		return
	}
	if publicStatic {
		w.Header().Del("Vary")
	} else {
		w.Header().Set("Vary", "Accept, X-Runtime, X-GOSH-Runtime")
	}
	if !publicStatic && isRuntimeRequest(r) {
		a.streamPage(w, r, page, params, status, result)
		return
	}
	runtimeURL, styleURLs := a.assetLinks(page, result)
	if !publicStatic {
		a.sendEarlyHints(w, r, runtimeURL, styleURLs)
	}
	if status == http.StatusOK && cacheState != "bypass" {
		key := routeRenderCacheKey(r)
		entry, found := a.routeCache.get(key)
		if found && entry.Document == nil {
			document, buildErr := a.buildCachedDocument(page, status, result, publicStatic)
			if buildErr != nil {
				a.renderHTTPError(w, r, http.StatusInternalServerError, buildErr.Error())
				return
			}
			a.routeCache.setDocument(key, document)
			entry.Document = document
		}
		if entry.Document != nil {
			body, materializeErr := a.materializeCachedDocument(w, r, entry.Document)
			if materializeErr != nil {
				a.renderHTTPError(w, r, http.StatusInternalServerError, materializeErr.Error())
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			a.setServerTiming(w, requestStarted, renderedAt)
			if publicStatic {
				w.Header().Set("Cache-Control", publicCacheControl(rule))
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if publicStatic && r.Header.Get("Authorization") == "" {
				if writeNotModifiedForHTML(w, r, body) {
					return
				}
			}
			w.WriteHeader(status)
			_, _ = w.Write(body)
			return
		}
	}
	if publicStatic {
		result.HTML = removeRuntimeStateAttributes(result.HTML)
		result.TeleportHTML = removeRuntimeStateAttributes(result.TeleportHTML)
		body, staticErr := a.renderBase(result, page, status, "", "", true)
		if staticErr != nil {
			a.renderHTTPError(w, r, http.StatusInternalServerError, staticErr.Error())
			return
		}
		setPublicStaticCSPHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.setServerTiming(w, requestStarted, renderedAt)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("Cache-Control", publicCacheControl(rule))
			if writeNotModifiedForHTML(w, r, body) {
				return
			}
		} else {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}
	nonce, err := newCSPNonce()
	if err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	setCSPHeaders(w, nonce)
	stateToken, err := a.bindRuntimeState(w, r, &result)
	if err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := a.renderBase(result, page, status, nonce, stateToken, false)
	if err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	a.setServerTiming(w, requestStarted, renderedAt)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (a *App) setServerTiming(w http.ResponseWriter, started, rendered time.Time) {
	if !a.config.Render.ServerTiming {
		return
	}
	now := time.Now()
	renderMS := float64(rendered.Sub(started).Microseconds()) / 1000
	documentMS := float64(now.Sub(rendered).Microseconds()) / 1000
	totalMS := float64(now.Sub(started).Microseconds()) / 1000
	w.Header().Set("Server-Timing", fmt.Sprintf("gosh-render;dur=%.2f, gosh-document;dur=%.2f, gosh-total;dur=%.2f", renderMS, documentMS, totalMS))
}

func (a *App) contentHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/post/")
	if name == "" || strings.Contains(name, "..") {
		a.renderHTTPError(w, r, http.StatusNotFound, "post not found")
		return
	}
	content := a.content
	if tenant, err := a.tenantWebFor(r); err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	} else if tenant != nil {
		if post, found := tenant.content[name]; found {
			content = map[string]contentPage{name: post}
		}
	}
	post, ok := content[name]
	if !ok {
		a.renderHTTPError(w, r, http.StatusNotFound, "post not found")
		return
	}
	page := &Page{RelativePath: "@content/" + name}
	contentKey := "content|" + tenantIDFromRequest(r) + "|" + name + "|" + hashText(post.Title+"\x00"+post.Description+"\x00"+post.HTML)
	if !isRuntimeRequest(r) && !isSiteSearchRequest(r) {
		if entry, found := a.routeCache.get(contentKey); found && entry.Document != nil && a.localRenderCacheValid(r.Context(), entry) {
			body, materializeErr := a.materializeCachedDocument(w, r, entry.Document)
			if materializeErr != nil {
				a.renderHTTPError(w, r, http.StatusInternalServerError, materializeErr.Error())
				return
			}
			w.Header().Set("X-MyelophOne-Cache", "hit")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "private, no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
	}
	props := requestProps(r, map[string]any{})
	for key, value := range a.config.Values {
		if _, exists := props[key]; !exists {
			props[key] = value
		}
	}
	props["config"] = a.config.Values
	props["title"] = post.Title
	props["description"] = post.Description
	props["image"] = post.Image
	res := RenderResult{HTML: `<main class="web-content">` + post.HTML + `</main>`, Data: props}
	res.SEO = logic.SEOState{SEOInput: logic.SEOInput{Title: post.Title, Description: post.Description, Image: post.Image}, Set: true}
	ctx := logicContext(r, map[string]any{})
	a.localizeContext(ctx, r)
	ctx.SetSEODefaults(res.SEO.SEOInput)
	ctx.SetFullscreenPreloaderDefaults(a.config.Preloader)
	applyFullscreenPreloaderData(&res, ctx)
	layoutName := a.config.Content.Layout
	if layoutName == "" {
		layoutName = a.config.Render.DefaultLayout
	}
	if layoutName != "" && layoutName != "none" {
		renderer, layouts, _, renderErr := a.renderingForRequest(r)
		if renderErr != nil {
			a.renderHTTPError(w, r, http.StatusInternalServerError, renderErr.Error())
			return
		}
		if layout, exists := layouts.Get(layoutName); exists {
			if rendered, err := renderer.RenderLayout(layout, res, ctx); err == nil {
				rendered.SEO = res.SEO
				res = rendered
			} else {
				a.renderer.logError("render content layout %s: %v", layoutName, err)
			}
		} else {
			a.renderer.logError("render content layout %s: not found", layoutName)
		}
	}
	renderer, _, teleports, renderErr := a.renderingForRequest(r)
	if renderErr != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, renderErr.Error())
		return
	}
	if err := appendTeleports(&res, ctx, renderer, teleports); err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	a.addBaseCSS(&res)
	if isSiteSearchRequest(r) {
		writeSiteSearchDocument(w, http.StatusOK, r, res)
		return
	}
	if isRuntimeRequest(r) {
		a.streamPage(w, r, page, map[string]any{}, http.StatusOK, res)
		return
	}
	a.setRouteCache(r.Context(), contentKey, res)
	document, err := a.buildCachedDocument(page, http.StatusOK, res, false)
	if err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	a.routeCache.setDocument(contentKey, document)
	body, err := a.materializeCachedDocument(w, r, document)
	if err != nil {
		a.renderHTTPError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (a *App) renderHTTPError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if a.errorRenderer != nil {
		a.errorRenderer(w, r, status, message)
		return
	}
	http.Error(w, message, status)
}

func appendTeleports(result *RenderResult, ctx *logic.Context, renderer *Renderer, teleports *Components) error {
	if teleports == nil || teleports.Len() == 0 {
		return nil
	}
	cssParts := map[string]bool{}
	for _, css := range result.CSSParts {
		cssParts[css] = true
	}
	appendCSS := func(css string) {
		if strings.TrimSpace(css) == "" || cssParts[css] {
			return
		}
		cssParts[css] = true
		result.CSSParts = append(result.CSSParts, css)
		result.CSS += css
		if !strings.HasSuffix(result.CSS, "\n") {
			result.CSS += "\n"
		}
	}
	for _, name := range teleports.Names() {
		component, _ := teleports.Get(name)
		var rendered RenderResult
		var err error
		if name == "RouteAnnouncer" {
			rendered, err = renderer.RenderComponentContext(component, Props{}, ctx)
		} else {
			wrapper := &Component{Name: "Teleport:" + name, Template: []Node{&ElementNode{Tag: "ClientOnly", Children: []Node{&ElementNode{Tag: name, IsComponent: true}}}}}
			rendered, err = renderer.RenderComponentContext(wrapper, Props{}, ctx)
		}
		if err != nil {
			return fmt.Errorf("render teleport %s: %w", name, err)
		}
		rendered.HTML = strings.ReplaceAll(rendered.HTML, `class="gosh-system-loader"`, `class="gosh-system-loader gosh-system-teleport-loader"`)
		if name != "RouteAnnouncer" && teleportStaticCacheable(component, renderer.components) {
			assetID := teleportAssetID(name, "")
			if ctx != nil && ctx.Request != nil {
				assetID = teleportAssetID(name, tenantIDFromRequest(ctx.Request))
			}
			rendered.HTML = strings.Replace(rendered.HTML, ` data-gosh-lazy="1"`, ` data-gosh-lazy="1" data-gosh-lazy-cache="1" data-gosh-lazy-src="/_gosh/teleport/`+assetID+`.json"`, 1)
		}
		result.TeleportHTML += rendered.HTML
		appendCSS(rendered.CSS)
		for _, css := range rendered.CSSParts {
			appendCSS(css)
		}
		if renderer != nil && renderer.components != nil {
			for _, view := range reachableViewsForPage(&Page{View: component, Layout: "none"}, renderer.components, nil, "") {
				appendCSS(view.CSS())
			}
		}
		if result.RuntimeStates == nil {
			result.RuntimeStates = map[string]Props{}
		}
		for token, props := range rendered.RuntimeStates {
			result.RuntimeStates[token] = props
		}
	}
	return nil
}

func (a *App) teleportHandler(w http.ResponseWriter, r *http.Request) {
	assetID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/_gosh/teleport/"), ".json")
	if assetID == "" || strings.Contains(assetID, "/") {
		http.NotFound(w, r)
		return
	}
	renderer, _, teleports, err := a.renderingForRequest(r)
	if err != nil || teleports == nil {
		http.NotFound(w, r)
		return
	}
	var name string
	var component *Component
	for _, candidate := range teleports.Names() {
		if teleportAssetID(candidate, tenantIDFromRequest(r)) != assetID {
			continue
		}
		name = candidate
		component, _ = teleports.Get(candidate)
		break
	}
	if name == "" || component == nil {
		http.NotFound(w, r)
		return
	}
	if name == "RouteAnnouncer" || !teleportStaticCacheable(component, renderer.components) {
		http.NotFound(w, r)
		return
	}
	ctx := &logic.Context{Request: r, Path: r.URL.RequestURI(), Params: map[string]any{}, ParamPartsMap: map[string][]string{}}
	result, err := renderer.RenderComponentContext(component, Props{}, ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	plan := a.modulePlan("@teleport/"+name, result.Scripts, false)
	payload, err := json.Marshal(map[string]any{"html": result.HTML, "modules": plan})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	etag := `"` + hashText(string(payload)) + `"`
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(payload)
}

func teleportStaticCacheable(root *Component, components *Components) bool {
	for _, view := range reachableViewsForPage(&Page{View: root, Layout: "none"}, components, nil, "") {
		if view.Mode == ModeClient || len(view.ServerRefs) > 0 || view.HasRuntimeAction {
			return false
		}
	}
	return true
}

func teleportAssetID(name, tenant string) string {
	return hashText("teleport:" + tenant + ":" + name)
}

func mergeProps(dst Props, data logic.Data) {
	for k, v := range data {
		dst[k] = v
	}
}

func runAction(view *Component, ctx *logic.Context, action string, props Props) (bool, error) {
	lp := logic.Props{}
	for k, v := range props {
		lp[k] = v
	}
	handled := false
	for _, ref := range view.ServerRefs {
		h := ref.Handler
		if h == nil {
			var ok bool
			h, ok = logic.Resolve(ref.Export)
			if !ok {
				return false, fmt.Errorf("server export %q not registered", ref.Export)
			}
		}
		data, did, err := h.Action(ctx, action, lp)
		if err != nil {
			return false, err
		}
		if did {
			handled = true
			for k, v := range data {
				props[k] = v
				lp[k] = v
			}
		}
	}
	return handled, nil
}

func (a *App) actionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.Header.Get("X-Runtime") != "1" || r.Header.Get("X-GOSH-Runtime") != "action" {
		http.NotFound(w, r)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !isValidOrigin(origin, r.Host) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad action", 400)
		return
	}
	owner, err := a.runtimeOwner(w, r)
	if err != nil {
		http.Error(w, "runtime state unavailable", http.StatusInternalServerError)
		return
	}
	props, ok := a.runtimeState.Get(owner, req.State)
	if !ok {
		w.Header().Set("X-GOSH-State", "expired")
		http.Error(w, "runtime state expired; reload the page", http.StatusConflict)
		return
	}
	if len(req.Fields) > 0 {
		props["form"] = req.Fields
	}
	ctx := &logic.Context{Request: r, Path: req.URL, Params: map[string]any{}, ParamPartsMap: map[string][]string{}}
	renderer, _, _, renderErr := a.renderingForRequest(r)
	if renderErr != nil {
		http.Error(w, renderErr.Error(), http.StatusInternalServerError)
		return
	}
	var view *Component
	target := ""
	if req.Kind == "page" {
		pages := a.pages
		if tenant, tenantErr := a.tenantWebFor(r); tenantErr != nil {
			http.Error(w, tenantErr.Error(), http.StatusInternalServerError)
			return
		} else if tenant != nil && tenant.pages != nil {
			pages = tenant.pages
		}
		p, ok := pages.ByRelative(req.Name)
		if !ok {
			http.Error(w, "page not found", 404)
			return
		}
		view = p.View
		target = "#app"
	} else {
		c, ok := renderer.components.Get(req.Name)
		if !ok {
			http.Error(w, "component not found", 404)
			return
		}
		view = c
		target = `[data-gosh-instance="` + req.Instance + `"]`
	}
	if err := logic.CallHook(logic.HookServerActionBefore, &logic.HookPayload{App: logic.UseApp(), Key: req.Action, Data: req}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	handled, err := runAction(view, ctx, req.Action, props)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !handled {
		http.Error(w, "action not handled", 404)
		return
	}
	if err := logic.CallHook(logic.HookServerActionAfter, &logic.HookPayload{App: logic.UseApp(), Key: req.Action, Data: props}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	invalidatedTags := logic.RevalidationTags(ctx)
	if len(invalidatedTags) > 0 {
		a.routeCache.invalidateTags(invalidatedTags)
		a.invalidateSharedRenderTags(r.Context(), invalidatedTags)
	}
	res, err := renderer.RenderComponentContextTarget(view, props, ctx, target)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	stateToken, err := a.bindRuntimeState(w, r, &res)
	if err != nil {
		http.Error(w, "runtime state unavailable", http.StatusInternalServerError)
		return
	}
	s := newStream(w)
	s.Send(Frame{"v": 1, "type": "begin", "scope": "component", "target": target, "clear": false})
	if len(invalidatedTags) > 0 {
		s.Send(Frame{"v": 1, "type": "cache-invalidate", "tags": invalidatedTags})
	}
	if len(res.Stores) > 0 {
		s.Send(Frame{"v": 1, "type": "store-state", "stores": res.Stores})
	}
	if a.config.Render.SplitCSS && strings.TrimSpace(res.CSS) != "" {
		s.Send(a.styleLinkFrame(res.CSS))
	}
	var inlineCSS string
	res.HTML, inlineCSS = nonceStylesForHTML(res.HTML)
	if inlineCSS != "" {
		s.Send(Frame{"v": 1, "type": "style", "id": "inline-" + hashText(inlineCSS), "css": inlineCSS})
	}
	s.Send(Frame{"v": 1, "type": "html", "target": target, "mode": "inner", "html": res.HTML})
	s.Send(Frame{"v": 1, "type": "patch", "operations": []map[string]any{{"op": "attrs", "target": target, "attrs": map[string]any{"data-gosh-state": stateToken}}}})
	if plan := a.modulePlan(a.pageKeyForURL(req.URL), res.Scripts, false); !plan.Empty() {
		s.Send(Frame{"v": 1, "type": "modules", "scope": "component", "plan": plan})
	}
	s.Send(Frame{"v": 1, "type": "end", "target": target})
}

func (a *App) lazyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	renderer, _, _, renderErr := a.renderingForRequest(r)
	if renderErr != nil {
		http.Error(w, renderErr.Error(), http.StatusInternalServerError)
		return
	}
	owner, err := a.runtimeOwner(w, r)
	if err != nil {
		http.Error(w, "runtime state unavailable", http.StatusInternalServerError)
		return
	}
	props, ok := a.runtimeState.Get(owner, req.State)
	if !ok {
		w.Header().Set("X-GOSH-State", "expired")
		http.Error(w, "runtime state expired; reload the page", http.StatusConflict)
		return
	}
	component, ok := renderer.components.Get(req.Name)
	if !ok {
		http.Error(w, "component not found", 404)
		return
	}
	ctx := &logic.Context{Request: r, Path: req.URL, Params: map[string]any{}, ParamPartsMap: map[string][]string{}}
	target := `[data-gosh-instance="` + req.Instance + `"]`
	res, err := renderer.RenderComponentContextTarget(component, props, ctx, target)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	stateToken, err := a.bindRuntimeState(w, r, &res)
	if err != nil {
		http.Error(w, "runtime state unavailable", http.StatusInternalServerError)
		return
	}
	s := newStream(w)
	s.Send(Frame{"v": 1, "type": "begin", "scope": "component", "target": target, "clear": false})
	if len(res.Stores) > 0 {
		s.Send(Frame{"v": 1, "type": "store-state", "stores": res.Stores})
	}
	var inlineCSS string
	res.HTML, inlineCSS = nonceStylesForHTML(res.HTML)
	if inlineCSS != "" {
		s.Send(Frame{"v": 1, "type": "style", "id": "inline-" + hashText(inlineCSS), "css": inlineCSS})
	}
	s.Send(Frame{"v": 1, "type": "html", "target": target, "mode": "inner", "html": res.HTML})
	s.Send(Frame{"v": 1, "type": "patch", "operations": []map[string]any{{"op": "attrs", "target": target, "attrs": map[string]any{"data-gosh-lazy": nil, "data-gosh-state": stateToken}}}})
	if plan := a.modulePlan(a.pageKeyForURL(req.URL), res.Scripts, false); !plan.Empty() {
		s.Send(Frame{"v": 1, "type": "modules", "scope": "component", "plan": plan})
	}
	s.Send(Frame{"v": 1, "type": "end", "target": target})
}

func (a *App) assetHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/_gosh/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(parts[1], ".js")
	var src string
	var ok bool
	a.mu.Lock()
	switch parts[0] {
	case "module":
		src, ok = a.modules[id]
	case "chunk":
		src, ok = a.chunks[id]
	case "vitals":
		if a.config.Runtime.WebVitals && id == hashText(string(embeddedVitalsJS)) {
			src, ok = string(embeddedVitalsJS), true
		}
	case "websocket":
		if id == hashText(string(embeddedWebSocketJS)) {
			src, ok = string(embeddedWebSocketJS), true
		}
	}
	a.mu.Unlock()
	if !ok {
		goshAssetNotFound(w, r)
		return
	}
	serveCachedJS(w, r, []byte(src), true)
}

func (a *App) pageRuntimeHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/_gosh/bootstrap/"), ".js")
	owner, err := a.runtimeOwner(w, r)
	if err != nil {
		http.Error(w, "runtime state unavailable", http.StatusInternalServerError)
		return
	}
	if _, ok := a.runtimeState.Get(owner, token); !ok {
		http.Error(w, "runtime state expired; reload the page", http.StatusConflict)
		return
	}
	plan, ok := a.lookupPagePlan(token)
	if !ok {
		http.Error(w, "page runtime unavailable", http.StatusNotFound)
		return
	}
	payload, _ := json.Marshal(map[string]any{"modules": plan})
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write([]byte("export default "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte(";"))
}

func serveCachedJS(w http.ResponseWriter, r *http.Request, body []byte, immutable bool) {
	body = withGeneratedJSBanner(body)
	etag := `"` + hashText(string(body)) + `"`
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("ETag", etag)
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	}
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(body)
}

func writeNotModifiedForHTML(w http.ResponseWriter, r *http.Request, body []byte) bool {
	etag := `"` + hashText(string(body)) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

func (a *App) styleHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/_gosh/style/"), ".css")
	a.mu.Lock()
	css, ok := a.styles[id]
	a.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	css = withGeneratedAssetBanner(css)
	etag := `"` + id + `"`
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write([]byte(css))
}

func (a *App) clientEntryHandler(w http.ResponseWriter, r *http.Request) {
	serveCachedJS(w, r, a.clientEntry, true)
}

func AnalyzeScriptUsage(pages *Pages, components *Components) map[string]int {
	usage := map[string]int{}
	views := []*Page{}
	views = append(views, pages.items...)
	if pages.NotFound != nil {
		views = append(views, pages.NotFound)
	}
	if pages.ErrorPage != nil {
		views = append(views, pages.ErrorPage)
	}

	for _, page := range views {
		seenScripts := map[string]bool{}
		seenViews := map[string]bool{}
		var visitView func(*Component)
		var visitNodes func([]Node)
		visitNodes = func(nodes []Node) {
			for _, node := range nodes {
				e, ok := node.(*ElementNode)
				if !ok {
					continue
				}
				if e.IsComponent {
					if c, ok := components.Get(e.Tag); ok {
						visitView(c)
					}
				}
				visitNodes(e.Children)
			}
		}
		visitView = func(view *Component) {
			if view == nil || seenViews[view.Path] {
				return
			}
			seenViews[view.Path] = true
			for _, block := range view.Scripts {
				source := strings.TrimSpace(block.Content)
				if source != "" {
					seenScripts[hashText(source)] = true
				}
			}
			if view.PreparedSetupSource != "" {
				seenScripts[hashText(view.PreparedSetupSource)] = true
			}
			visitNodes(view.Template)
		}
		visitView(page.View)
		for id := range seenScripts {
			usage[id]++
		}
	}
	return usage
}

func sharedLayoutScriptSources(layouts *Components) map[string]string {
	sources := map[string]string{}
	if layouts == nil {
		return sources
	}
	for _, name := range layouts.Names() {
		layout, ok := layouts.Get(name)
		if !ok {
			continue
		}
		for _, block := range layout.Scripts {
			source := strings.TrimSpace(block.Content)
			if source != "" {
				sources[hashText(source)] = source
			}
		}
		if layout.PreparedSetupSource != "" {
			sources[hashText(layout.PreparedSetupSource)] = layout.PreparedSetupSource
		}
	}
	return sources
}

func sharedTeleportScriptSources(teleports, components *Components) map[string]string {
	sources := map[string]string{}
	seen := map[string]bool{}
	var visit func(*Component)
	var nodes func([]Node)
	visit = func(component *Component) {
		if component == nil || seen[component.Path] {
			return
		}
		seen[component.Path] = true
		for _, block := range component.Scripts {
			source := strings.TrimSpace(block.Content)
			if source != "" {
				sources[hashText(source)] = source
			}
		}
		if component.PreparedSetupSource != "" {
			sources[hashText(component.PreparedSetupSource)] = component.PreparedSetupSource
		}
		nodes(component.Template)
	}
	nodes = func(items []Node) {
		for _, node := range items {
			e, ok := node.(*ElementNode)
			if !ok {
				continue
			}
			if e.IsComponent && components != nil {
				if component, ok := components.Get(e.Tag); ok {
					visit(component)
				}
			}
			nodes(e.Children)
		}
	}
	if teleports != nil {
		for _, name := range teleports.Names() {
			if component, ok := teleports.Get(name); ok {
				visit(component)
			}
		}
	}
	return sources
}

func reusableComponentScriptSources(components *Components, usage map[string]int) map[string]string {
	sources := map[string]string{}
	if components == nil {
		return sources
	}
	for _, name := range components.Names() {
		component, ok := components.Get(name)
		if !ok {
			continue
		}
		for _, block := range component.Scripts {
			source := strings.TrimSpace(block.Content)
			id := hashText(source)
			if source != "" && usage[id] > 1 {
				sources[id] = source
			}
		}
		if component.PreparedSetupSource != "" {
			source := component.PreparedSetupSource
			id := hashText(source)
			if usage[id] > 1 {
				sources[id] = source
			}
		}
	}
	return sources
}

func sourceIDSet(sources map[string]string) map[string]bool {
	ids := make(map[string]bool, len(sources))
	for id := range sources {
		ids[id] = true
	}
	return ids
}

type DependencyGraph struct {
	Components    map[string]bool
	ServerExports map[string]ServerRef
}

func BuildDependencyGraph(pages *Pages, components *Components) *DependencyGraph {
	return BuildDependencyGraphWithLayouts(pages, components, nil, "")
}

func BuildDependencyGraphWithLayouts(pages *Pages, components, layouts *Components, defaultLayout string) *DependencyGraph {
	g := &DependencyGraph{Components: map[string]bool{}, ServerExports: map[string]ServerRef{}}
	seenViews := map[string]bool{}
	var visitView func(*Component)
	var visitNodes func([]Node)
	visitNodes = func(nodes []Node) {
		for _, node := range nodes {
			e, ok := node.(*ElementNode)
			if !ok {
				continue
			}
			if e.IsComponent {
				if c, ok := components.Get(e.Tag); ok && !g.Components[c.Name] {
					g.Components[c.Name] = true
					visitView(c)
				}
			}
			visitNodes(e.Children)
		}
	}
	visitView = func(view *Component) {
		if view == nil || seenViews[view.Path] {
			return
		}
		seenViews[view.Path] = true
		for _, ref := range view.ServerRefs {
			g.ServerExports[ref.Export] = ref
		}
		visitNodes(view.Template)
	}
	for _, p := range pageList(pages) {
		if layouts != nil {
			for _, view := range reachableViewsForPage(p, components, layouts, defaultLayout) {
				visitView(view)
			}
		} else {
			visitView(p.View)
		}
	}
	return g
}

func sortedGraphComponents(g *DependencyGraph) []string {
	out := make([]string, 0, len(g.Components))
	for k := range g.Components {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type TailwindBuild struct {
	CSSByPage map[string]string
	Sources   map[string][]string
	Binary    string
}

func pageList(pages *Pages) []*Page {
	out := append([]*Page(nil), pages.items...)
	if pages.NotFound != nil {
		out = append(out, pages.NotFound)
	}
	if pages.ErrorPage != nil {
		out = append(out, pages.ErrorPage)
	}
	return out
}

func reachableViewsForPage(page *Page, components, layouts *Components, defaultLayout string) []*Component {
	cookieControlEnabled := logic.MustRuntimeConfig().CookieControl.Enabled
	seen := map[string]bool{}
	var out []*Component
	var visitView func(*Component)
	var visitNodes func([]Node)

	visitNodes = func(nodes []Node) {
		for _, node := range nodes {
			e, ok := node.(*ElementNode)
			if !ok {
				continue
			}
			if e.IsComponent {
				if component, ok := components.Get(e.Tag); ok {
					visitView(component)
				}
			}
			visitNodes(e.Children)
		}
	}
	visitView = func(view *Component) {
		if view == nil || seen[view.Path] {
			return
		}
		if !cookieControlEnabled && disabledCookieGlobalComponent(view.Name) {
			return
		}
		seen[view.Path] = true
		out = append(out, view)
		visitNodes(view.Template)
	}

	visitView(page.View)
	layoutName := page.Layout
	if layoutName == "" {
		layoutName = defaultLayout
	}
	if layoutName != "" && layoutName != "none" && layouts != nil {
		if layout, ok := layouts.Get(layoutName); ok {
			visitView(layout)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func disabledCookieGlobalComponent(name string) bool {
	switch name {
	case "CookieBanner", "CookieSettingsModal":
		return true
	default:
		return false
	}
}

func tailwindSourceForPage(page *Page, components, layouts *Components, defaultLayout string) (string, []string) {
	views := reachableViewsForPage(page, components, layouts, defaultLayout)
	var b strings.Builder
	names := make([]string, 0, len(views))
	hasClientComponent := false
	for _, view := range views {
		if view.Mode == ModeClient {
			hasClientComponent = true
		}
		name := filepath.ToSlash(view.RelativePath)
		names = append(names, name)
		b.WriteString("\n<!-- GOSH reachable source: ")
		b.WriteString(name)
		b.WriteString(" -->\n")
		b.WriteString(view.Source)
		if !strings.HasSuffix(view.Source, "\n") {
			b.WriteByte('\n')
		}
	}
	if hasClientComponent {
		loaderPath := filepath.Join(systemTemplatesDir, "loader.gosh")
		if data, err := sourceReadFile(loaderPath); err == nil {
			names = append(names, "system/loader.gosh")
			b.WriteString("\n<!-- GOSH system loader -->\n")
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	return b.String(), names
}

func resolveTailwindBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MYELOPHONE_TAILWIND_BIN")); override != "" {
		path, err := filepath.Abs(override)
		if err == nil {
			if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
				return path, nil
			}
		}
		if lp, lookErr := exec.LookPath(override); lookErr == nil {
			return lp, nil
		}
		return "", fmt.Errorf("MYELOPHONE_TAILWIND_BIN=%q does not point to an executable", override)
	}

	localCandidates := []string{
		filepath.Join(systemTailwindDir, "node_modules", ".bin", "tailwindcss"),
		filepath.Join(systemTailwindDir, "node_modules", ".bin", "tailwindcss.cmd"),
	}
	for _, candidate := range localCandidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			path, _ := filepath.Abs(candidate)
			return path, nil
		}
	}
	if path, err := exec.LookPath("tailwindcss"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("Tailwind CSS v4 is required but the local CLI was not found; run `yarn --cwd web/system/tailwind install`, set MYELOPHONE_TAILWIND_BIN, or put a standalone `tailwindcss` executable in PATH")
}

func readTailwindGlobalCSS(root string) (string, error) {
	dir := filepath.Join(root, "styles")
	entries, err := sourceReadDir(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var out strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gosh") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := sourceReadFile(path)
		if err != nil {
			return "", err
		}
		source := string(data)
		blocks, parseErr := parseSFCBlocks(source)
		foundStyle := false
		if parseErr == nil {
			for _, block := range blocks {
				if block.Name != "style" {
					continue
				}
				foundStyle = true
				out.WriteString("\n/* global/styles/")
				out.WriteString(entry.Name())
				out.WriteString(" */\n")
				out.WriteString(block.Block.Content)
				out.WriteByte('\n')
			}
		}
		if !foundStyle && strings.TrimSpace(source) != "" {
			out.WriteString("\n/* global/styles/")
			out.WriteString(entry.Name())
			out.WriteString(" */\n")
			out.WriteString(source)
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func compileTailwind(binary, source, tailwindGlobalCSS, systemDefaultCSS, projectDefaultCSS, globalCSS string, minify bool) (string, error) {
	if err := os.MkdirAll(systemTailwindDir, 0o755); err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp(systemTailwindDir, ".myelophone-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	sourcePath := filepath.Join(tmpDir, "reachable.gosh")
	inputPath := filepath.Join(tmpDir, "input.css")
	outputPath := filepath.Join(tmpDir, "output.css")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		return "", err
	}

	input := `@import "tailwindcss" source(none);` + "\n"
	input += `@source "./reachable.gosh";` + "\n"
	if strings.TrimSpace(tailwindGlobalCSS) != "" {
		input += "\n/* web/system/tailwind/global.css */\n" + tailwindGlobalCSS + "\n"
	}
	if strings.TrimSpace(systemDefaultCSS) != "" {
		input += "\n/* web/system/css/default.css */\n" + systemDefaultCSS + "\n"
	}
	if strings.TrimSpace(projectDefaultCSS) != "" {
		input += "\n/* web/css/default.css */\n" + projectDefaultCSS + "\n"
	}
	if strings.TrimSpace(globalCSS) != "" {
		input += "\n/* web/global/styles/*.gosh */\n" + globalCSS + "\n"
	}
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		return "", err
	}

	inputAbs, err := filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return "", err
	}
	args := []string{"-i", inputAbs, "-o", outputAbs}
	if minify {
		args = append(args, "--minify")
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = systemTailwindDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("tailwindcss: %w: %s", err, message)
		}
		return "", fmt.Errorf("tailwindcss: %w", err)
	}
	css, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read Tailwind output: %w", err)
	}
	return processFinalCSS(string(css))
}

func resolveNodeBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MYELOPHONE_NODE_BIN")); override != "" {
		if path, err := filepath.Abs(override); err == nil {
			if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
				return path, nil
			}
		}
		if path, err := exec.LookPath(override); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("MYELOPHONE_NODE_BIN=%q does not point to an executable", override)
	}
	if path, err := exec.LookPath("node"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("final CSS processing requires Node.js; install Node or set MYELOPHONE_NODE_BIN")
}

func processFinalCSS(css string) (string, error) {
	node, err := resolveNodeBinary()
	if err != nil {
		return "", err
	}
	runner, err := filepath.Abs(filepath.Join(systemTailwindDir, "postcss-runner.mjs"))
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(runner); err != nil || st.IsDir() {
		return "", fmt.Errorf("final CSS PostCSS runner is missing: %s", runner)
	}
	tmpDir, err := os.MkdirTemp(systemTailwindDir, ".myelophone-postcss-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	input := filepath.Join(tmpDir, "input.css")
	output := filepath.Join(tmpDir, "output.css")
	if err := os.WriteFile(input, []byte(css), 0o600); err != nil {
		return "", err
	}
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(node, runner, inputAbs, outputAbs)
	cmd.Dir = systemTailwindDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("final CSS PostCSS: %w: %s", err, message)
		}
		return "", fmt.Errorf("final CSS PostCSS: %w", err)
	}
	processed, err := os.ReadFile(output)
	if err != nil {
		return "", fmt.Errorf("read final PostCSS output: %w", err)
	}
	return string(processed), nil
}

func BuildTailwind(pages *Pages, components, layouts *Components, cfg logic.RuntimeConfig) (TailwindBuild, error) {
	result := TailwindBuild{CSSByPage: map[string]string{}, Sources: map[string][]string{}}
	binary, err := resolveTailwindBinary()
	if err != nil {
		return result, err
	}
	result.Binary = binary
	globalCSS, err := readTailwindGlobalCSS(systemGlobalDir)
	if err != nil {
		return result, fmt.Errorf("tailwind global styles: %w", err)
	}
	tailwindGlobalCSS, err := loadOptionalCSS(systemTailwindGlobal)
	if err != nil {
		return result, fmt.Errorf("tailwind global CSS: %w", err)
	}
	systemDefaultCSS, err := loadOptionalCSS(filepath.Join(systemFrameworkCSSDir, "default.css"))
	if err != nil {
		return result, fmt.Errorf("system default CSS: %w", err)
	}
	projectDefaultCSS, err := loadOptionalCSS(filepath.Join(systemWebCSSDir, "default.css"))
	if err != nil {
		return result, fmt.Errorf("project default CSS: %w", err)
	}

	var combinedSource strings.Builder
	for _, page := range pageList(pages) {
		source, names := tailwindSourceForPage(page, components, layouts, cfg.Render.DefaultLayout)
		result.Sources[page.RelativePath] = names
		combinedSource.WriteString(source)
		combinedSource.WriteByte('\n')
	}
	if combinedSource.Len() == 0 {
		return result, nil
	}
	css, err := compileTailwind(binary, combinedSource.String(), tailwindGlobalCSS, systemDefaultCSS, projectDefaultCSS, globalCSS, cfg.Tailwind.Minify)
	if err != nil {
		return result, fmt.Errorf("compile shared Tailwind CSS: %w", err)
	}
	result.CSSByPage["@shared"] = css
	return result, nil
}

func (a *App) addTailwind(page *Page, result *RenderResult) {
}

func (a *App) addBaseCSS(result *RenderResult) {}

func responseCacheable(h http.Header, status int) bool {
	cc := strings.ToLower(h.Get("Cache-Control"))
	return status >= 200 && status < 300 && h.Get("Set-Cookie") == "" && !strings.Contains(cc, "no-store") && !strings.Contains(cc, "private")
}
func (a *App) executeCachedRoute(route logic.Route, r *http.Request) cachedHTTPResponse {
	rec := newCaptureWriter()
	route.Handler(a.responder, rec, r)
	return cachedHTTPResponse{Status: rec.status, Header: cloneHeader(rec.header), Body: append([]byte(nil), rec.body.Bytes()...), Stored: time.Now()}
}
func (a *App) serveServerRoute(route logic.Route, w http.ResponseWriter, r *http.Request) {
	rule, ok := a.routeRule(r.URL.Path)
	ttl, swr := routeTTL(rule)
	if !ok || ttl <= 0 || !cacheableRequest(r) {
		route.Handler(a.responder, w, r)
		return
	}
	key := tenantIDFromRequest(r) + "|" + r.Method + " " + r.URL.RequestURI()
	if ent, found := a.apiCache.get(key); found {
		age := time.Since(ent.Stored)
		if age < time.Duration(ttl)*time.Second {
			_ = logic.CallHook(logic.HookServerCacheHit, &logic.HookPayload{App: logic.UseApp(), Key: key, Data: "api"})
			writeCachedHTTP(w, ent, "hit")
			return
		}
		if swr > 0 && age < time.Duration(ttl+swr)*time.Second {
			if a.apiCache.begin(key) {
				clone := r.Clone(context.WithoutCancel(r.Context()))
				task := logic.BindBackgroundApp(func() {
					defer a.apiCache.end(key)
					fresh := a.executeCachedRoute(route, clone)
					if responseCacheable(fresh.Header, fresh.Status) {
						a.apiCache.set(key, fresh)
					}
				})
				go task()
			}
			_ = logic.CallHook(logic.HookServerCacheStale, &logic.HookPayload{App: logic.UseApp(), Key: key, Data: "api"})
			writeCachedHTTP(w, ent, "stale")
			return
		}
	}
	value, err, _ := a.apiCache.flights.Do(key, func() (any, error) {
		if ent, found := a.apiCache.get(key); found && time.Since(ent.Stored) < time.Duration(ttl)*time.Second {
			return ent, nil
		}
		_ = logic.CallHook(logic.HookServerCacheMiss, &logic.HookPayload{App: logic.UseApp(), Key: key, Data: "api"})
		fresh := a.executeCachedRoute(route, r)
		if responseCacheable(fresh.Header, fresh.Status) {
			a.apiCache.set(key, fresh)
		}
		return fresh, nil
	})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeCachedHTTP(w, value.(cachedHTTPResponse), "miss")
}

func (a *App) RegisterRoutes(s *Server) {
	if s == nil {
		return
	}
	a.registerRoutes(func(method, path string, handler http.HandlerFunc) {
		addServerRoute(s, method, path, a.wrapRequest(http.HandlerFunc(handler)).ServeHTTP)
	})
	a.registerServerFallback(s.router)

	assetHandler := a.wrapRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Clone(r.Context())
		request.URL.Path = strings.TrimPrefix(r.URL.Path, "/assets")
		if a.serveContentResource(w, request) {
			return
		}
		s.PublicFiles(w, request)
	})).ServeHTTP
	s.GET("/assets/*path", assetHandler)
	s.HEAD("/assets/*path", assetHandler)
}

func (a *App) serveContentResource(w http.ResponseWriter, r *http.Request) bool {
	if _, production := sourceFS(); production {
		return false
	}
	path := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
	root := ""
	relative := ""
	if strings.HasPrefix(path, "content/") {
		root, relative = systemContentDir, strings.TrimPrefix(path, "content/")
	} else if strings.HasPrefix(path, "tenants/") {
		parts := strings.Split(path, "/")
		if len(parts) < 4 || parts[0] != "tenants" || parts[2] != "content" || !safeTenantID(parts[1]) || tenantIDFromRequest(r) != parts[1] {
			return false
		}
		root, relative = filepath.Join(tenantRootDir, parts[1], "content"), strings.Join(parts[3:], "/")
	} else {
		return false
	}
	if relative == "" || strings.EqualFold(filepath.Ext(relative), ".md") {
		return false
	}
	file := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	if !pathWithin(file, root) {
		return false
	}
	data, err := sourceReadFile(file)
	if err != nil {
		return false
	}
	http.ServeContent(w, r, filepath.Base(file), time.Time{}, bytes.NewReader(data))
	return true
}

func (a *App) registerServerFallback(router *Router) {
	if router == nil {
		return
	}
	for _, route := range logic.ServerRoutes() {
		route := route
		router.HandleFallback(route.Method, route.Path, a.wrapRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := route.Handler(&logic.Event{Writer: w, Request: r, Params: RouteParams(r)}); err != nil {
				http.Error(w, err.Error(), logic.ErrorStatus(err))
			}
		})))
	}
}

func (a *App) registerRoutes(register func(method, path string, handler http.HandlerFunc)) {
	register(http.MethodGet, a.clientURL, a.clientEntryHandler)
	register(http.MethodGet, "/_gosh/style/*path", a.styleHandler)
	if a.config.SiteSearch.Enabled {
		register(http.MethodGet, "/_gosh/site-search", a.siteSearchStreamHandler)
		register(http.MethodGet, "/_gosh/site-search/query", a.siteSearchQueryHandler)
	}
	register(http.MethodPost, "/_gosh/action", a.actionHandler)
	register(http.MethodPost, "/_gosh/lazy", a.lazyHandler)
	register(http.MethodGet, "/_gosh/teleport/*path", a.teleportHandler)
	register(http.MethodGet, "/_gosh/bootstrap/*path", a.pageRuntimeHandler)
	register(http.MethodGet, "/_gosh/module/*path", a.assetHandler)
	register(http.MethodGet, "/_gosh/chunk/*path", a.assetHandler)
	register(http.MethodGet, "/_gosh/vitals/*path", a.assetHandler)
	register(http.MethodGet, "/_gosh/websocket/*path", a.assetHandler)
	register(http.MethodGet, "/_gosh/*path", goshAssetNotFound)
	for _, route := range logic.Routes() {
		route := route
		register(route.Method, route.Path, func(w http.ResponseWriter, r *http.Request) {
			a.serveServerRoute(route, w, r)
		})
	}
}

func goshAssetNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(r.URL.Path, ".js") {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else if strings.HasSuffix(r.URL.Path, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.Error(w, "GOSH internal asset not found", http.StatusNotFound)
}

func addServerRoute(s *Server, method, path string, handler http.HandlerFunc) {
	switch method {
	case http.MethodGet:
		s.GET(path, handler)
	case http.MethodPost:
		s.POST(path, handler)
	case http.MethodPut:
		s.PUT(path, handler)
	case http.MethodDelete:
		s.DELETE(path, handler)
	case http.MethodHead:
		s.HEAD(path, handler)
	case http.MethodPatch:
		s.PATCH(path, handler)
	case http.MethodOptions:
		s.OPTIONS(path, handler)
	default:
		s.addRoute(method, path, handler)
	}
}

func (a *App) wrapRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appCtx, cleanup := logic.EnterRequest(w, r, a.config)
		defer cleanup()
		defer func() {
			if recovered := recover(); recovered != nil {
				err := fmt.Errorf("panic: %v", recovered)
				appCtx.Error = err
				_ = logic.CallHook(logic.HookServerError, &logic.HookPayload{App: appCtx, Error: err})
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		if err := logic.SetupRequestPlugins(appCtx); err != nil {
			appCtx.Error = err
			_ = logic.CallHook(logic.HookServerError, &logic.HookPayload{App: appCtx, Error: err})
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := logic.CallHook(logic.HookServerRequestBefore, &logic.HookPayload{App: appCtx}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = logic.CallHook(logic.HookServerRequestAfter, &logic.HookPayload{App: appCtx}) }()
		next.ServeHTTP(w, r)
	})
}

func newCSPNonce() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

func setCSPHeaders(w http.ResponseWriter, nonce string) {
	policy := "default-src 'self' https: data: blob:; " +
		"script-src 'nonce-" + nonce + "' 'strict-dynamic'; " +
		"script-src-elem 'self' 'nonce-" + nonce + "'; " +
		"style-src 'self' https: 'nonce-" + nonce + "'; " +
		"style-src-attr 'none'; " +
		"img-src 'self' https: data: blob:; font-src 'self' https: data:; media-src 'self' https: blob:; " +
		"frame-src 'self' https:; connect-src 'self' https: wss:; worker-src 'self' blob:; " +
		"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'"
	w.Header().Set("Content-Security-Policy", policy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

func setPublicStaticCSPHeaders(w http.ResponseWriter) {
	policy := "default-src 'self' https: data: blob:; " +
		"script-src 'none'; " +
		"style-src 'self' https: 'unsafe-inline'; style-src-attr 'none'; " +
		"img-src 'self' https: data: blob:; font-src 'self' https: data:; media-src 'self' https: blob:; " +
		"frame-src 'self' https:; connect-src 'self' https: wss:; worker-src 'self' blob:; " +
		"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'"
	w.Header().Set("Content-Security-Policy", policy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

func addNonceToTrustedHTML(body []byte, nonce string) []byte {
	if nonce == "" || len(body) == 0 {
		return body
	}
	s := string(body)
	n := html.EscapeString(nonce)
	var out strings.Builder
	out.Grow(len(s) + 32)
	for offset := 0; offset < len(s); {
		scriptAt := strings.Index(s[offset:], "<script")
		styleAt := strings.Index(s[offset:], "<style")
		nextAt, tag := -1, ""
		if scriptAt >= 0 {
			nextAt, tag = offset+scriptAt, "script"
		}
		if styleAt >= 0 && (nextAt < 0 || offset+styleAt < nextAt) {
			nextAt, tag = offset+styleAt, "style"
		}
		if nextAt < 0 {
			out.WriteString(s[offset:])
			break
		}
		out.WriteString(s[offset:nextAt])
		tagEnd := strings.IndexByte(s[nextAt:], '>')
		if tagEnd < 0 {
			out.WriteString(s[nextAt:])
			break
		}
		tagEnd += nextAt
		head := s[nextAt : tagEnd+1]
		if !strings.Contains(head, " nonce=") && !strings.Contains(head, " nonce\t=") {
			prefix := "<" + tag
			out.WriteString(prefix)
			out.WriteString(` nonce="`)
			out.WriteString(n)
			out.WriteByte('"')
			out.WriteString(strings.TrimPrefix(head, prefix))
		} else {
			out.WriteString(head)
		}
		offset = tagEnd + 1
	}
	return []byte(out.String())
}
