package goserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type HTMLElement struct {
	Node *html.Node
}

type Soft404Rules struct {
	Contains []string
	Exact    []string
}

var ErrSoft404 = errors.New("soft 404 detected")

func ParseHTML(htmlStr string) (*HTMLElement, error) {
	node, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return nil, err
	}
	return &HTMLElement{Node: node}, nil
}

func ParseHTMLFragment(htmlStr string) ([]*HTMLElement, error) {
	nodes, err := html.ParseFragment(strings.NewReader(htmlStr), &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil {
		return nil, err
	}

	var elements []*HTMLElement
	for _, n := range nodes {
		elements = append(elements, &HTMLElement{Node: n})
	}
	return elements, nil
}

func (e *HTMLElement) GetAttribute(name string) string {
	if e == nil || e.Node == nil {
		return ""
	}
	for _, attr := range e.Node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func (e *HTMLElement) SetAttribute(name, value string) {
	if e == nil || e.Node == nil {
		return
	}
	for i, attr := range e.Node.Attr {
		if attr.Key == name {
			e.Node.Attr[i].Val = value
			return
		}
	}
	e.Node.Attr = append(e.Node.Attr, html.Attribute{Key: name, Val: value})
}

func (e *HTMLElement) RemoveAttribute(name string) {
	if e == nil || e.Node == nil {
		return
	}
	var newAttrs []html.Attribute
	for _, attr := range e.Node.Attr {
		if attr.Key != name {
			newAttrs = append(newAttrs, attr)
		}
	}
	e.Node.Attr = newAttrs
}

func (e *HTMLElement) TextContent() string {
	if e == nil || e.Node == nil {
		return ""
	}
	var text string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			text += n.Data
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(e.Node)
	return strings.TrimSpace(text)
}

func (e *HTMLElement) InnerHTML() string {
	if e == nil || e.Node == nil {
		return ""
	}
	var buf bytes.Buffer
	for c := e.Node.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&buf, c)
	}
	return buf.String()
}

func (e *HTMLElement) SetInnerHTML(htmlStr string) error {
	if e == nil || e.Node == nil {
		return errors.New("nil element")
	}

	for c := e.Node.FirstChild; c != nil; {
		next := c.NextSibling
		e.Node.RemoveChild(c)
		c = next
	}

	newElements, err := ParseHTMLFragment(htmlStr)
	if err != nil {
		return err
	}
	for _, child := range newElements {
		e.Node.AppendChild(child.Node)
	}
	return nil
}

func (e *HTMLElement) OuterHTML() string {
	if e == nil || e.Node == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = html.Render(&buf, e.Node)
	return buf.String()
}

func (e *HTMLElement) AppendChild(child *HTMLElement) {
	if e != nil && e.Node != nil && child != nil && child.Node != nil {
		e.Node.AppendChild(child.Node)
	}
}

func (e *HTMLElement) Remove() {
	if e != nil && e.Node != nil && e.Node.Parent != nil {
		e.Node.Parent.RemoveChild(e.Node)
	}
}

func (e *HTMLElement) GetElementById(id string) *HTMLElement {
	var result *HTMLElement
	e.traverse(func(n *html.Node) bool {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == id {
				result = &HTMLElement{Node: n}
				return true
			}
		}
		return false
	})
	return result
}

type selectorRule struct {
	tag     string
	id      string
	classes []string
}

func parseSelector(s string) selectorRule {
	var rule selectorRule
	var current strings.Builder
	mode := 't'

	commit := func() {
		val := current.String()
		if val == "" {
			return
		}
		switch mode {
		case 't':
			rule.tag = val
		case 'i':
			rule.id = val
		case 'c':
			rule.classes = append(rule.classes, val)
		}
		current.Reset()
	}

	for _, ch := range s {
		switch ch {
		case '#':
			commit()
			mode = 'i'
		case '.':
			commit()
			mode = 'c'
		default:
			current.WriteRune(ch)
		}
	}
	commit()
	return rule
}

func (r selectorRule) matches(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}

	if r.tag != "" && n.Data != r.tag {
		return false
	}

	if r.id != "" && getAttr(n, "id") != r.id {
		return false
	}

	if len(r.classes) > 0 {
		nodeClasses := strings.Fields(getAttr(n, "class"))
		for _, requiredClass := range r.classes {
			found := false
			for _, nc := range nodeClasses {
				if nc == requiredClass {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func (e *HTMLElement) QuerySelectorAll(selector string) []*HTMLElement {
	if e == nil || e.Node == nil {
		return nil
	}

	parts := strings.Fields(selector)
	if len(parts) == 0 {
		return nil
	}

	findDescendants := func(root *html.Node, rule selectorRule) []*html.Node {
		var found []*html.Node
		var traverse func(*html.Node)

		traverse = func(n *html.Node) {
			if n != root && rule.matches(n) {
				found = append(found, n)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverse(c)
			}
		}
		traverse(root)
		return found
	}

	currentRoots := []*html.Node{e.Node}

	for _, part := range parts {
		rule := parseSelector(part)
		var nextRoots []*html.Node
		seen := make(map[*html.Node]bool)

		for _, root := range currentRoots {
			matched := findDescendants(root, rule)
			for _, m := range matched {
				if !seen[m] {
					seen[m] = true
					nextRoots = append(nextRoots, m)
				}
			}
		}

		currentRoots = nextRoots
		if len(currentRoots) == 0 {
			return nil
		}
	}

	var results []*HTMLElement
	for _, n := range currentRoots {
		results = append(results, &HTMLElement{Node: n})
	}
	return results
}

func (e *HTMLElement) QuerySelector(selector string) *HTMLElement {
	elements := e.QuerySelectorAll(selector)
	if len(elements) > 0 {
		return elements[0]
	}
	return nil
}

func (e *HTMLElement) traverse(cb func(*html.Node) bool) bool {
	if e == nil || e.Node == nil {
		return false
	}
	if cb(e.Node) {
		return true
	}
	for c := e.Node.FirstChild; c != nil; c = c.NextSibling {
		childWrapper := &HTMLElement{Node: c}
		if childWrapper.traverse(cb) {
			return true
		}
	}
	return false
}

func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func CreateElement(tag string) *HTMLElement {
	node := &html.Node{
		Type: html.ElementNode,
		Data: tag,
	}
	node.DataAtom = atom.Lookup([]byte(tag))

	return &HTMLElement{Node: node}
}

func CreateTextNode(text string) *HTMLElement {
	return &HTMLElement{
		Node: &html.Node{
			Type: html.TextNode,
			Data: text,
		},
	}
}

func (e *HTMLElement) InsertBefore(newChild *HTMLElement) error {
	if e == nil || e.Node == nil || e.Node.Parent == nil {
		return errors.New("cannot insert before: element has no parent")
	}
	if newChild == nil || newChild.Node == nil {
		return errors.New("cannot insert nil child")
	}
	e.Node.Parent.InsertBefore(newChild.Node, e.Node)
	return nil
}

func (e *HTMLElement) InsertAfter(newChild *HTMLElement) error {
	if e == nil || e.Node == nil || e.Node.Parent == nil {
		return errors.New("cannot insert after: element has no parent")
	}
	if newChild == nil || newChild.Node == nil {
		return errors.New("cannot insert nil child")
	}
	if e.Node.NextSibling == nil {
		e.Node.Parent.AppendChild(newChild.Node)
	} else {
		e.Node.Parent.InsertBefore(newChild.Node, e.Node.NextSibling)
	}
	return nil
}

func (e *HTMLElement) InsertHTML(position string, htmlStr string) error {
	if e == nil || e.Node == nil {
		return errors.New("nil element")
	}

	newNodes, err := ParseHTMLFragment(htmlStr)
	if err != nil {
		return err
	}
	if len(newNodes) == 0 {
		return nil
	}

	position = strings.ToLower(position)

	for _, childWrapper := range newNodes {
		child := childWrapper.Node

		switch position {
		case "beforebegin":
			if e.Node.Parent == nil {
				return errors.New("no parent node to insert beforebegin")
			}
			e.Node.Parent.InsertBefore(child, e.Node)

		case "afterbegin":
			if e.Node.FirstChild != nil {
				e.Node.InsertBefore(child, e.Node.FirstChild)
			} else {
				e.Node.AppendChild(child)
			}

		case "beforeend":
			e.Node.AppendChild(child)

		case "afterend":
			if e.Node.Parent == nil {
				return errors.New("no parent node to insert afterend")
			}
			if e.Node.NextSibling != nil {
				e.Node.Parent.InsertBefore(child, e.Node.NextSibling)
			} else {
				e.Node.Parent.AppendChild(child)
			}

		default:
			return fmt.Errorf("invalid position: %s", position)
		}
	}

	return nil
}

func CheckSoft404(resp *http.Response, rules Soft404Rules) error {
	if resp == nil || resp.Body == nil {
		return nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrSoft404
	}

	z := html.NewTokenizer(io.LimitReader(resp.Body, 256*1024))

	for {
		tt := z.Next()

		if tt == html.ErrorToken {
			return nil
		}

		if tt == html.StartTagToken {
			t := z.Token()

			if t.Data == "title" {
				tt = z.Next()

				var rawTitle string

				for tt == html.TextToken {
					rawTitle += z.Token().Data
					tt = z.Next()
				}

				titleText := strings.TrimSpace(rawTitle)
				titleLower := strings.ToLower(titleText)

				for _, exactWord := range rules.Exact {
					w := strings.TrimSpace(strings.ToLower(exactWord))

					if w == "" {
						continue
					}

					if titleLower == w {
						return ErrSoft404
					}
				}

				for _, containsWord := range rules.Contains {
					w := strings.TrimSpace(strings.ToLower(containsWord))

					if w == "" {
						continue
					}

					if strings.Contains(titleLower, w) {
						return ErrSoft404
					}
				}

				return nil
			}

			if t.Data == "body" {
				return nil
			}
		}
	}
}

func (hc *HttpClient) CheckStatus(ctx context.Context, url string, headers ...map[string]string) (int, error) {
	reqHead, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	applyHeaders(reqHead, headers)

	resp, err := hc.Do(reqHead)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		return resp.StatusCode, nil
	}

	reqGet, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	applyHeaders(reqGet, headers)

	reqGet.Header.Set("Range", "bytes=0-0")

	resp, err = hc.Do(reqGet)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent {
		return http.StatusOK, nil
	}

	return resp.StatusCode, nil
}
