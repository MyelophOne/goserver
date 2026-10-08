package goserver

import (
	"bytes"
	"encoding/json"
	"html"
	stdtemplate "html/template"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/tdewolff/minify/v2"
	cssmin "github.com/tdewolff/minify/v2/css"
	jsmin "github.com/tdewolff/minify/v2/js"
	parse "github.com/tdewolff/parse/v2"
	jsparse "github.com/tdewolff/parse/v2/js"
	htmlscan "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func (r *Renderer) logError(format string, args ...any) {
	if r != nil && r.logger != nil {
		r.logger.Printf("ERROR: "+format, args...)
		return
	}
	log.Printf("ERROR: "+format, args...)
}

func finalizeTrustedHTML(document []byte, nonce string) []byte {
	scanner := htmlscan.NewTokenizer(bytes.NewReader(document))
	out := make([]byte, 0, len(document)+128)
	escaped := html.EscapeString(nonce)
	rawKind := ""
	preDepth := 0
	pendingSpace := false
	blockBoundary := true
	for {
		kind := scanner.Next()
		raw := scanner.Raw()
		if kind == htmlscan.ErrorToken {
			out = append(out, raw...)
			return out
		}
		var name []byte
		var more bool
		if kind == htmlscan.StartTagToken || kind == htmlscan.SelfClosingTagToken || kind == htmlscan.EndTagToken {
			// TagName copies the token; ordinary lowercase tags can use its raw bytes.
			start := 1
			if kind == htmlscan.EndTagToken {
				start++
			}
			end := start
			uppercase := false
			for end < len(raw) && !isHTMLSpace(raw[end]) && raw[end] != '\f' && raw[end] != '>' && raw[end] != '/' {
				uppercase = uppercase || raw[end] >= 'A' && raw[end] <= 'Z'
				end++
			}
			name = raw[start:end]
			if uppercase || bytes.Equal(name, []byte("script")) || bytes.Equal(name, []byte("style")) {
				name, more = scanner.TagName()
			}
		}
		tag := atom.Lookup(name)
		if pendingSpace && kind != htmlscan.TextToken {
			if !blockBoundary && !htmlBlockBoundary(tag) {
				out = append(out, "&#32;"...)
			}
			pendingSpace = false
		}
		if kind == htmlscan.TextToken {
			if rawKind == "script" || rawKind == "style" || rawKind == "json" {
				out = append(out, compactRawSource(rawKind, raw)...)
			} else if preDepth > 0 || rawKind == "textarea" {
				out = appendSingleLineText(out, raw)
			} else {
				textStart := len(out)
				for _, b := range raw {
					if isHTMLSpace(b) || b == '\f' {
						if len(out) > 0 && out[len(out)-1] != ' ' {
							out = append(out, ' ')
						}
					} else {
						out = append(out, b)
					}
				}
				if len(out) == textStart+1 && out[textStart] == ' ' {
					out = out[:textStart]
					pendingSpace = true
				}
			}
			continue
		}
		if kind == htmlscan.StartTagToken || kind == htmlscan.SelfClosingTagToken || kind == htmlscan.EndTagToken {
			blockBoundary = htmlBlockBoundary(tag)
		} else if kind == htmlscan.DoctypeToken {
			blockBoundary = true
		}
		if kind == htmlscan.EndTagToken {
			if bytes.Equal(name, []byte("pre")) && preDepth > 0 {
				preDepth--
			}
			rawKind = ""
		}
		if kind == htmlscan.StartTagToken || kind == htmlscan.SelfClosingTagToken {
			if bytes.Equal(name, []byte("pre")) {
				preDepth++
			}
			rawKind = ""
			switch string(name) {
			case "script":
				rawKind = "script"
			case "style":
				rawKind = "style"
			case "textarea":
				rawKind = "textarea"
			}
			if bytes.Equal(name, []byte("script")) || bytes.Equal(name, []byte("style")) {
				hasNonce := false
				for more {
					var key, value []byte
					key, value, more = scanner.TagAttr()
					if bytes.Equal(key, []byte("type")) && (bytes.Contains(value, []byte("json")) || bytes.Equal(value, []byte("importmap"))) {
						rawKind = "json"
					}
					if bytes.Equal(key, []byte("nonce")) {
						hasNonce = true
					}
				}
				if !hasNonce && nonce != "" {
					end := 1
					for end < len(raw) && !isHTMLSpace(raw[end]) && raw[end] != '>' && raw[end] != '/' {
						end++
					}
					out = append(out, raw[:end]...)
					out = append(out, ` nonce="`...)
					out = append(out, escaped...)
					out = append(out, '"')
					out = appendSingleLineTag(out, raw[end:])
					continue
				}
			}
		}
		if kind == htmlscan.CommentToken {
			raw = bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("\r"), []byte(" ")), []byte("\n"), []byte(" "))
		}
		out = appendSingleLineTag(out, raw)
	}
}

// Preserve inline word separators while omitting document and block indentation.
func htmlBlockBoundary(tag atom.Atom) bool {
	switch tag {
	case atom.Html, atom.Head, atom.Body, atom.Meta, atom.Link, atom.Title, atom.Script, atom.Style,
		atom.Address, atom.Article, atom.Aside, atom.Blockquote, atom.Details, atom.Dialog, atom.Div, atom.Dl, atom.Dt, atom.Dd,
		atom.Fieldset, atom.Figcaption, atom.Figure, atom.Footer, atom.Form, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Header, atom.Hgroup, atom.Hr, atom.Li, atom.Main, atom.Nav, atom.Ol, atom.P, atom.Pre, atom.Section, atom.Summary,
		atom.Table, atom.Thead, atom.Tbody, atom.Tfoot, atom.Tr, atom.Td, atom.Th, atom.Ul:
		return true
	}
	return false
}

func appendSingleLineTag(out, source []byte) []byte {
	if !bytes.ContainsAny(source, "\r\n") {
		return append(out, source...)
	}
	var quote byte
	for _, b := range source {
		if quote == 0 && (b == '\'' || b == '"') {
			quote = b
		} else if b == quote {
			quote = 0
		}
		if b == '\n' || b == '\r' {
			if quote == 0 {
				out = append(out, ' ')
			} else if b == '\n' {
				out = append(out, "&#10;"...)
			} else {
				out = append(out, "&#13;"...)
			}
		} else {
			out = append(out, b)
		}
	}
	return out
}

func appendSingleLineText(out, source []byte) []byte {
	for _, b := range source {
		switch b {
		case '\n':
			out = append(out, "&#10;"...)
		case '\r':
			out = append(out, "&#13;"...)
		default:
			out = append(out, b)
		}
	}
	return out
}

var sourceMinifier = func() *minify.M {
	m := minify.New()
	m.AddFunc("script", jsmin.Minify)
	m.AddFunc("style", cssmin.Minify)
	return m
}()
var compactSources = struct {
	sync.Mutex
	items map[string]string
}{items: make(map[string]string)}

func compactRawSource(kind string, source []byte) string {
	if kind == "json" {
		var out bytes.Buffer
		if json.Compact(&out, source) == nil {
			return out.String()
		}
		return string(source)
	}
	if !bytes.ContainsAny(source, "\r\n") {
		return string(source)
	}
	key := kind + string(source)
	compactSources.Lock()
	cached, ok := compactSources.items[key]
	compactSources.Unlock()
	if ok {
		return cached
	}
	result, err := sourceMinifier.String(kind, string(source))
	if err != nil {
		return string(source)
	}
	if kind == "script" && strings.ContainsAny(result, "\r\n") {
		ast, parseErr := jsparse.Parse(parse.NewInputString(result), jsparse.Options{})
		check := &taggedTemplateCheck{}
		if parseErr == nil {
			jsparse.Walk(check, ast)
		}
		if parseErr == nil && !check.tagged {
			result = strings.ReplaceAll(strings.ReplaceAll(result, "\r", "\\r"), "\n", "\\n")
		}
	}
	compactSources.Lock()
	if len(compactSources.items) < 256 && len(key) < 65536 {
		compactSources.items[key] = result
	}
	compactSources.Unlock()
	return result
}

type taggedTemplateCheck struct{ tagged bool }

func (v *taggedTemplateCheck) Enter(node jsparse.INode) jsparse.IVisitor {
	if t, ok := node.(*jsparse.TemplateExpr); ok && t.Tag != nil {
		v.tagged = true
	}
	return v
}
func (*taggedTemplateCheck) Exit(jsparse.INode) {}

func safeDynamicURL(value string) bool {
	normalized := strings.Map(func(r rune) rune {
		if r <= 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	scheme, _, absolute := strings.Cut(normalized, ":")
	if !absolute || strings.ContainsAny(scheme, "/?#") {
		return true
	}
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto", "tel":
		return true
	}
	return false
}

func staticRenderTree(element *ElementNode) bool {
	if element.IsComponent || elementIsSlot(element) || elementIsClientOnly(element) {
		return false
	}
	for _, attr := range element.Attrs {
		if attr.Dynamic || attr.Event || attr.Directive || isStructuralDirective(attr.Name) {
			return false
		}
	}
	for _, node := range element.Children {
		switch child := node.(type) {
		case *TextNode, *CommentNode:
		case *ElementNode:
			if !staticRenderTree(child) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func prepareRawTextExpressions(element *ElementNode) {
	if !strings.EqualFold(element.Tag, "script") && !strings.EqualFold(element.Tag, "style") {
		return
	}
	var source strings.Builder
	source.WriteString("<" + element.Tag + ">")
	var expressions []string
	for _, node := range element.Children {
		switch n := node.(type) {
		case *TextNode:
			source.WriteString(n.Text)
		case *InterpolationNode:
			source.WriteString("{{.V" + strconv.Itoa(len(expressions)) + "}}")
			expressions = append(expressions, n.Expression)
		default:
			return
		}
	}
	if len(expressions) == 0 {
		return
	}
	source.WriteString("</" + element.Tag + ">")
	compiled, err := stdtemplate.New("raw-context").Parse(source.String())
	if err == nil {
		element.contextTemplate = compiled
		element.contextExpressions = expressions
	}
}
