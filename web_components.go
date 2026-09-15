package goserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	logic "github.com/myelophone/goserver/web/runtime"
)

func LoadComponents(root string) (*Components, error) {
	walkRoot := root
	baseRoot := root
	if _, prod := sourceFS(); !prod {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve components root: %w", err)
		}
		walkRoot, baseRoot = absRoot, absRoot
	}

	components := &Components{root: baseRoot, items: make(map[string]*Component)}

	err := sourceWalkDir(walkRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".gosh") {
			return nil
		}

		component, err := ParseComponentFile(baseRoot, filePath)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filePath, err)
		}

		key := normalizeComponentLookup(component.Name)
		if previous, exists := components.items[key]; exists {
			return fmt.Errorf(
				"duplicate component name %q:\n  %s\n  %s",
				component.Name,
				previous.Path,
				component.Path,
			)
		}

		components.items[key] = component
		components.names = append(components.names, component.Name)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(components.names)
	return components, nil
}

func ParseComponentFile(componentsRoot, path string) (*Component, error) {
	data, err := sourceReadFile(path)
	if err != nil {
		return nil, err
	}

	relativePath, err := filepath.Rel(componentsRoot, path)
	if err != nil {
		return nil, err
	}

	source := string(data)
	blocks, err := parseSFCBlocks(source)
	if err != nil {
		return nil, err
	}

	component := &Component{
		Name:         NuxtComponentName(relativePath),
		Path:         path,
		RelativePath: filepath.ToSlash(relativePath),
		Source:       source,
		Mode:         componentModeFromPath(relativePath),
		ServerRefs:   resolveServerRefs(parseServerRefs(source)),
		Layout:       parseMetaValue(source, "@layout"),
	}

	var templateSeen bool
	var hasScopedStyle bool

	for _, parsed := range blocks {
		switch parsed.Name {
		case "template":
			if templateSeen {
				return nil, fmt.Errorf("multiple <template> blocks")
			}
			templateSeen = true
			component.TemplateSource = parsed.Block.Content

		case "script":
			if _, setup := parsed.Block.Attrs["setup"]; setup {
				if component.ScriptSetup != nil {
					return nil, fmt.Errorf("multiple <script setup> blocks")
				}
				block := parsed.Block
				component.ScriptSetup = &block
			} else {
				component.Scripts = append(component.Scripts, parsed.Block)
			}

		case "head":
			component.Head += parsed.Block.Content

		case "style":
			_, scoped := parsed.Block.Attrs["scoped"]
			_, module := parsed.Block.Attrs["module"]
			style := StyleBlock{
				Content: parsed.Block.Content,
				Attrs:   parsed.Block.Attrs,
				Scoped:  scoped,
				Module:  module,
				Lang:    parsed.Block.Attrs["lang"],
			}
			if scoped {
				hasScopedStyle = true
			}
			component.Styles = append(component.Styles, style)
		}
	}

	if len(component.Scripts) > 0 {
		if component.Mode == ModeServer {
			return nil, fmt.Errorf("%s is .server.gosh and cannot contain <script>", component.RelativePath)
		}
		return nil, fmt.Errorf("%s uses unsupported <script>; use <script setup>", component.RelativePath)
	}

	if hasScopedStyle {
		component.ScopeID = makeScopeID(component.RelativePath)
	}

	if strings.TrimSpace(component.TemplateSource) != "" {
		parser := newTemplateParser(component.TemplateSource)
		component.Template, err = parser.Parse()
		if err != nil {
			return nil, fmt.Errorf("template: %w", err)
		}

		if component.ScopeID != "" {
			applyScope(component.Template, component.ScopeID)
		}
	}
	prepareTemplateMetadata(component.Template)

	if component.ScriptSetup != nil && strings.TrimSpace(component.ScriptSetup.Content) != "" {
		component.PreparedSetupSource = setupScriptSource(component.ScriptSetup.Content)
	}
	component.HasRuntimeAction = componentHasRuntimeAction(component.Template)

	return component, nil
}

func resolveServerRefs(refs []ServerRef) []ServerRef {
	for i := range refs {
		if handler, ok := logic.Resolve(refs[i].Export); ok {
			refs[i].Handler = handler
		}
	}
	return refs
}

func componentModeFromPath(path string) ComponentMode {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(base, ".client.gosh"):
		return ModeClient
	case strings.HasSuffix(base, ".server.gosh"):
		return ModeServer
	default:
		return ModeUniversal
	}
}

func parseMetaValue(source, key string) string {
	for _, line := range strings.Split(source, "\n") {
		i := strings.Index(line, key)
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(line[i+len(key):])
		rest = strings.Trim(rest, "-*/ \t")
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

func parseServerRefs(source string) []ServerRef {
	var refs []ServerRef
	for _, line := range strings.Split(source, "\n") {
		i := strings.Index(line, "@server")
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(line[i+len("@server"):])
		rest = strings.Trim(rest, "-*/ \t")
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		spec := fields[0]
		j := strings.LastIndex(spec, "#")
		if j <= 0 || j == len(spec)-1 {
			continue
		}
		refs = append(refs, ServerRef{Path: spec[:j], Export: spec[j+1:]})
	}
	return refs
}

func normalizeComponentLookup(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func makeScopeID(relativePath string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(relativePath)))
	return "data-v-" + hex.EncodeToString(sum[:4])
}

func applyScope(nodes []Node, scopeID string) {
	for _, node := range nodes {
		element, ok := node.(*ElementNode)
		if !ok {
			continue
		}

		if !element.IsComponent {
			element.ScopeAttrs = appendUnique(element.ScopeAttrs, scopeID)
		}

		applyScope(element.Children, scopeID)
	}
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

type templateParser struct {
	source string
	pos    int
}

func newTemplateParser(source string) *templateParser {
	return &templateParser{source: source}
}

func (p *templateParser) Parse() ([]Node, error) {
	nodes, closing, err := p.parseNodes("")
	if err != nil {
		return nil, err
	}
	if closing != "" {
		return nil, fmt.Errorf("unexpected closing tag </%s>", closing)
	}
	return nodes, nil
}

func (p *templateParser) parseNodes(expectedClosing string) ([]Node, string, error) {
	var nodes []Node

	for p.pos < len(p.source) {
		if strings.HasPrefix(p.source[p.pos:], "<!--") {
			comment, err := p.parseComment()
			if err != nil {
				return nil, "", err
			}
			nodes = append(nodes, comment)
			continue
		}

		if strings.HasPrefix(p.source[p.pos:], "{{") {
			expr, err := p.parseInterpolation()
			if err != nil {
				return nil, "", err
			}
			nodes = append(nodes, expr)
			continue
		}

		if p.source[p.pos] == '<' {
			if strings.HasPrefix(p.source[p.pos:], "</") {
				closing, err := p.parseClosingTag()
				if err != nil {
					return nil, "", err
				}

				if expectedClosing == "" {
					return nodes, closing, nil
				}
				if !sameTag(closing, expectedClosing) {
					return nil, "", fmt.Errorf(
						"expected </%s>, got </%s>",
						expectedClosing,
						closing,
					)
				}
				return nodes, closing, nil
			}

			if strings.HasPrefix(p.source[p.pos:], "<!") {
				text, err := p.parseDeclaration()
				if err != nil {
					return nil, "", err
				}
				nodes = append(nodes, parsedTextNode(text))
				continue
			}

			element, err := p.parseElement()
			if err != nil {
				return nil, "", err
			}
			nodes = append(nodes, element)
			continue
		}

		text := p.parseText()
		if text != "" {
			nodes = append(nodes, parsedTextNode(text))
		}
	}

	if expectedClosing != "" {
		return nil, "", fmt.Errorf("unclosed <%s>", expectedClosing)
	}

	return nodes, "", nil
}

func parsedTextNode(text string) *TextNode {
	return &TextNode{Text: text, Prepared: true, Whitespace: strings.TrimSpace(text) == ""}
}

func (p *templateParser) parseElement() (*ElementNode, error) {
	if p.source[p.pos] != '<' {
		return nil, fmt.Errorf("internal parser error: expected '<' at byte %d", p.pos)
	}

	end := findTagEnd(p.source, p.pos)
	if end < 0 {
		return nil, fmt.Errorf("unclosed opening tag at byte %d", p.pos)
	}

	raw := p.source[p.pos+1 : end]
	p.pos = end + 1

	raw = strings.TrimSpace(raw)
	selfClosing := strings.HasSuffix(raw, "/")
	if selfClosing {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	}

	tag, rest := splitTagName(raw)
	if tag == "" {
		return nil, fmt.Errorf("empty tag at byte %d", p.pos)
	}

	attrs, err := parseTemplateAttributes(rest)
	if err != nil {
		return nil, fmt.Errorf("<%s>: %w", tag, err)
	}

	element := &ElementNode{
		Tag:         tag,
		IsComponent: isComponentTag(tag),
		SelfClosing: selfClosing || isVoidHTMLTag(tag),
		Attrs:       attrs,
	}

	if element.SelfClosing {
		return element, nil
	}

	children, _, err := p.parseNodes(tag)
	if err != nil {
		return nil, err
	}
	element.Children = children
	return element, nil
}

func (p *templateParser) parseClosingTag() (string, error) {
	start := p.pos
	end := strings.IndexByte(p.source[start:], '>')
	if end < 0 {
		return "", fmt.Errorf("unclosed closing tag at byte %d", start)
	}
	end += start

	raw := strings.TrimSpace(p.source[start+2 : end])
	if raw == "" {
		return "", fmt.Errorf("empty closing tag at byte %d", start)
	}

	name, _ := splitTagName(raw)
	p.pos = end + 1
	return name, nil
}

func (p *templateParser) parseComment() (*CommentNode, error) {
	start := p.pos + len("<!--")
	endOffset := strings.Index(p.source[start:], "-->")
	if endOffset < 0 {
		return nil, fmt.Errorf("unclosed HTML comment at byte %d", p.pos)
	}
	end := start + endOffset
	text := p.source[start:end]
	p.pos = end + len("-->")
	return &CommentNode{Text: text}, nil
}

func (p *templateParser) parseInterpolation() (*InterpolationNode, error) {
	start := p.pos + 2
	endOffset := strings.Index(p.source[start:], "}}")
	if endOffset < 0 {
		return nil, fmt.Errorf("unclosed interpolation at byte %d", p.pos)
	}
	end := start + endOffset
	expr := strings.TrimSpace(p.source[start:end])
	p.pos = end + 2
	return &InterpolationNode{Expression: expr}, nil
}

func (p *templateParser) parseDeclaration() (string, error) {
	start := p.pos
	end := strings.IndexByte(p.source[start:], '>')
	if end < 0 {
		return "", fmt.Errorf("unclosed declaration at byte %d", start)
	}
	end += start
	p.pos = end + 1
	return p.source[start:p.pos], nil
}

func (p *templateParser) parseText() string {
	start := p.pos
	for p.pos < len(p.source) {
		if p.source[p.pos] == '<' || strings.HasPrefix(p.source[p.pos:], "{{") {
			break
		}
		p.pos++
	}
	return p.source[start:p.pos]
}

func splitTagName(raw string) (name, rest string) {
	i := 0
	for i < len(raw) && !unicode.IsSpace(rune(raw[i])) {
		i++
	}
	return raw[:i], strings.TrimSpace(raw[i:])
}

func parseTemplateAttributes(input string) ([]Attribute, error) {
	var attrs []Attribute
	pos := 0

	for pos < len(input) {
		for pos < len(input) && unicode.IsSpace(rune(input[pos])) {
			pos++
		}
		if pos >= len(input) {
			break
		}

		start := pos
		for pos < len(input) && !unicode.IsSpace(rune(input[pos])) && input[pos] != '=' {
			pos++
		}
		name := input[start:pos]
		if name == "" {
			return nil, fmt.Errorf("invalid attribute at byte %d", pos)
		}

		for pos < len(input) && unicode.IsSpace(rune(input[pos])) {
			pos++
		}

		attr := Attribute{
			Name:      name,
			Dynamic:   strings.HasPrefix(name, ":") || strings.HasPrefix(name, "v-bind:"),
			Event:     strings.HasPrefix(name, "@") || strings.HasPrefix(name, "v-on:"),
			Directive: strings.HasPrefix(name, ":") || strings.HasPrefix(name, "@") || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "v-"),
		}

		if pos >= len(input) || input[pos] != '=' {
			attr.Boolean = true
			attrs = append(attrs, attr)
			continue
		}

		pos++
		for pos < len(input) && unicode.IsSpace(rune(input[pos])) {
			pos++
		}
		if pos >= len(input) {
			return nil, fmt.Errorf("attribute %q has no value", name)
		}

		if input[pos] == '\'' || input[pos] == '"' {
			quote := input[pos]
			pos++
			valueStart := pos
			for pos < len(input) && input[pos] != quote {
				pos++
			}
			if pos >= len(input) {
				return nil, fmt.Errorf("attribute %q has unclosed quoted value", name)
			}
			attr.Value = input[valueStart:pos]
			pos++
		} else {
			valueStart := pos
			for pos < len(input) && !unicode.IsSpace(rune(input[pos])) {
				pos++
			}
			attr.Value = input[valueStart:pos]
		}

		attrs = append(attrs, attr)
	}

	return attrs, nil
}

func isComponentTag(tag string) bool {
	if tag == "" {
		return false
	}

	first, _ := utf8FirstRune(tag)
	if unicode.IsUpper(first) {
		return true
	}

	return strings.Contains(tag, "-") && !isKnownHTMLTag(tag)
}

func utf8FirstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

func sameTag(a, b string) bool {
	return strings.EqualFold(a, b)
}

func isVoidHTMLTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func isKnownHTMLTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "html", "head", "body", "title", "base", "link", "meta", "style",
		"article", "section", "nav", "aside", "h1", "h2", "h3", "h4", "h5", "h6",
		"header", "footer", "address", "main", "p", "hr", "pre", "blockquote", "ol", "ul", "menu", "li",
		"dl", "dt", "dd", "figure", "figcaption", "div", "a", "em", "strong", "small", "s", "cite", "q",
		"dfn", "abbr", "ruby", "rt", "rp", "data", "time", "code", "var", "samp", "kbd", "sub", "sup",
		"i", "b", "u", "mark", "bdi", "bdo", "span", "br", "wbr", "ins", "del", "picture", "source",
		"img", "iframe", "embed", "object", "video", "audio", "track", "map", "table", "caption", "colgroup",
		"col", "tbody", "thead", "tfoot", "tr", "td", "th", "form", "label", "input", "button", "select",
		"datalist", "optgroup", "option", "textarea", "output", "progress", "meter", "fieldset", "legend",
		"details", "summary", "dialog", "slot", "template", "canvas", "noscript":
		return true
	default:
		return false
	}
}

func scopeCSS(css, scopeID string) string {
	return rewriteCSSRules(css, scopeID)
}

func rewriteCSSRules(css, scopeID string) string {
	var out strings.Builder
	pos := 0

	for pos < len(css) {
		open := findCSSOpenBrace(css, pos)
		if open < 0 {
			out.WriteString(css[pos:])
			break
		}

		header := css[pos:open]
		close := findMatchingBrace(css, open)
		if close < 0 {
			out.WriteString(css[pos:])
			break
		}

		body := css[open+1 : close]
		trimmed := strings.TrimSpace(header)

		if strings.HasPrefix(trimmed, "@") {
			lower := strings.ToLower(trimmed)
			out.WriteString(header)
			out.WriteByte('{')

			if strings.HasPrefix(lower, "@media") ||
				strings.HasPrefix(lower, "@supports") ||
				strings.HasPrefix(lower, "@layer") ||
				strings.HasPrefix(lower, "@container") {
				out.WriteString(rewriteCSSRules(body, scopeID))
			} else {
				out.WriteString(body)
			}

			out.WriteByte('}')
		} else {
			out.WriteString(rewriteSelectorList(header, scopeID))
			out.WriteByte('{')
			out.WriteString(body)
			out.WriteByte('}')
		}

		pos = close + 1
	}

	return out.String()
}

func findCSSOpenBrace(css string, start int) int {
	var quote byte
	inComment := false

	for i := start; i < len(css); i++ {
		if inComment {
			if i+1 < len(css) && css[i] == '*' && css[i+1] == '/' {
				inComment = false
				i++
			}
			continue
		}

		if quote != 0 {
			if css[i] == '\\' {
				i++
				continue
			}
			if css[i] == quote {
				quote = 0
			}
			continue
		}

		if i+1 < len(css) && css[i] == '/' && css[i+1] == '*' {
			inComment = true
			i++
			continue
		}

		switch css[i] {
		case '\'', '"':
			quote = css[i]
		case '{':
			return i
		}
	}

	return -1
}

func findMatchingBrace(css string, open int) int {
	depth := 0
	var quote byte
	inComment := false

	for i := open; i < len(css); i++ {
		if inComment {
			if i+1 < len(css) && css[i] == '*' && css[i+1] == '/' {
				inComment = false
				i++
			}
			continue
		}

		if quote != 0 {
			if css[i] == '\\' {
				i++
				continue
			}
			if css[i] == quote {
				quote = 0
			}
			continue
		}

		if i+1 < len(css) && css[i] == '/' && css[i+1] == '*' {
			inComment = true
			i++
			continue
		}

		switch css[i] {
		case '\'', '"':
			quote = css[i]
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func rewriteSelectorList(header, scopeID string) string {
	prefix, selectors := splitLeadingWhitespace(header)
	parts := splitCSSSelectors(selectors)
	for i := range parts {
		parts[i] = scopeOneSelector(strings.TrimSpace(parts[i]), scopeID)
	}
	return prefix + strings.Join(parts, ", ")
}

func splitLeadingWhitespace(s string) (string, string) {
	i := 0
	for i < len(s) && unicode.IsSpace(rune(s[i])) {
		i++
	}
	return s[:i], s[i:]
}

func splitCSSSelectors(s string) []string {
	var parts []string
	start := 0
	paren := 0
	bracket := 0
	var quote byte

	for i := 0; i < len(s); i++ {
		c := s[i]
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

		switch c {
		case '\'', '"':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case ',':
			if paren == 0 && bracket == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func scopeOneSelector(selector, scopeID string) string {
	if selector == "" {
		return selector
	}

	if strings.HasPrefix(selector, ":global(") && strings.HasSuffix(selector, ")") {
		return strings.TrimSuffix(strings.TrimPrefix(selector, ":global("), ")")
	}

	return appendScopeToFinalCompound(selector, scopeID)
}

func appendScopeToFinalCompound(selector, scopeID string) string {
	lastStart := 0
	paren := 0
	bracket := 0
	var quote byte

	for i := 0; i < len(selector); i++ {
		c := selector[i]
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
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		default:
			if paren == 0 && bracket == 0 {
				if c == '>' || c == '+' || c == '~' {
					lastStart = i + 1
				} else if unicode.IsSpace(rune(c)) {
					j := i
					for j < len(selector) && unicode.IsSpace(rune(selector[j])) {
						j++
					}
					if j < len(selector) && selector[j] != '>' && selector[j] != '+' && selector[j] != '~' {
						lastStart = j
					}
				}
			}
		}
	}

	prefix := selector[:lastStart]
	compound := selector[lastStart:]
	compound = strings.TrimLeftFunc(compound, unicode.IsSpace)
	if compound == "" {
		return selector
	}

	insert := len(compound)
	paren = 0
	bracket = 0
	quote = 0
	for i := 0; i < len(compound); i++ {
		c := compound[i]
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
		switch c {
		case '\'', '"':
			quote = c
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ':':
			if paren == 0 && bracket == 0 {
				insert = i
				i = len(compound)
			}
		}
	}

	return prefix + compound[:insert] + "[" + scopeID + "]" + compound[insert:]
}

type parsedBlock struct {
	Name  string
	Block Block
}

func parseSFCBlocks(source string) ([]parsedBlock, error) {
	var blocks []parsedBlock
	offset := 0

	for offset < len(source) {
		start, name, attrsText, contentStart, ok := findNextTopLevelBlock(source, offset)
		if !ok {
			break
		}

		closeTag := "</" + name + ">"
		closeOffset := strings.Index(source[contentStart:], closeTag)
		if closeOffset < 0 {
			return nil, fmt.Errorf("unclosed <%s> starting at byte %d", name, start)
		}

		contentEnd := contentStart + closeOffset
		blocks = append(blocks, parsedBlock{
			Name: name,
			Block: Block{
				Content: source[contentStart:contentEnd],
				Attrs:   parseBlockAttributes(attrsText),
			},
		})

		offset = contentEnd + len(closeTag)
	}

	return blocks, nil
}

func findNextTopLevelBlock(source string, offset int) (start int, name, attrs string, contentStart int, ok bool) {
	names := []string{"template", "script", "style", "head"}
	best := -1
	bestName := ""

	for _, candidate := range names {
		needle := "<" + candidate
		searchFrom := offset

		for searchFrom < len(source) {
			index := strings.Index(source[searchFrom:], needle)
			if index < 0 {
				break
			}
			index += searchFrom

			after := index + len(needle)
			if after == len(source) || unicode.IsSpace(rune(source[after])) || source[after] == '>' {
				if best == -1 || index < best {
					best = index
					bestName = candidate
				}
				break
			}
			searchFrom = after
		}
	}

	if best < 0 {
		return 0, "", "", 0, false
	}

	openEnd := findTagEnd(source, best)
	if openEnd < 0 {
		return 0, "", "", 0, false
	}

	nameEnd := best + 1 + len(bestName)
	attrsText := strings.TrimSpace(source[nameEnd:openEnd])
	return best, bestName, attrsText, openEnd + 1, true
}

func findTagEnd(source string, start int) int {
	var quote byte

	for i := start; i < len(source); i++ {
		c := source[i]

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

		switch c {
		case '\'', '"':
			quote = c
		case '>':
			return i
		}
	}

	return -1
}

func parseBlockAttributes(input string) map[string]string {
	result := make(map[string]string)
	attrs, err := parseTemplateAttributes(input)
	if err != nil {
		return result
	}
	for _, attr := range attrs {
		result[attr.Name] = attr.Value
	}
	return result
}

func NuxtComponentName(relativePath string) string {
	path := filepath.ToSlash(relativePath)
	path = strings.TrimSuffix(path, filepath.Ext(path))

	path = strings.TrimSuffix(path, ".client")
	path = strings.TrimSuffix(path, ".server")
	path = strings.TrimSuffix(path, ".global")

	rawParts := strings.Split(path, "/")
	parts := make([]string, 0, len(rawParts))

	for _, part := range rawParts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") {
			continue
		}
		parts = append(parts, toPascalCase(part))
	}

	if len(parts) == 0 {
		return ""
	}

	result := parts[0]
	for _, part := range parts[1:] {
		if strings.HasPrefix(strings.ToLower(part), strings.ToLower(result)) {
			result = part
			continue
		}
		if strings.HasSuffix(strings.ToLower(result), strings.ToLower(part)) {
			continue
		}
		result += part
	}

	return result
}

func toPascalCase(value string) string {
	var result strings.Builder
	var word strings.Builder

	flush := func() {
		if word.Len() == 0 {
			return
		}
		runes := []rune(word.String())
		runes[0] = unicode.ToUpper(runes[0])
		result.WriteString(string(runes))
		word.Reset()
	}

	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	return result.String()
}
