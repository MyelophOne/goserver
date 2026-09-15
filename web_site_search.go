package goserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"

	logic "github.com/myelophone/goserver/web/runtime"
)

const siteSearchHeader = "X-GOSH-Site-Search"

type siteSearchDocument struct {
	Path        string   `json:"path,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Links       []string `json:"links"`
	NoIndex     bool     `json:"noIndex"`
}

type siteSearchResponseCapture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *siteSearchResponseCapture) Header() http.Header    { return w.header }
func (w *siteSearchResponseCapture) WriteHeader(status int) { w.status = status }
func (w *siteSearchResponseCapture) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func isSiteSearchRequest(r *http.Request) bool { return r.Header.Get(siteSearchHeader) == "1" }

func newSiteSearchDocument(path string, result RenderResult) siteSearchDocument {
	document := siteSearchDocument{
		Path:        path,
		Title:       strings.TrimSpace(stringValue(result.SEO.Title)),
		Description: strings.TrimSpace(stringValue(result.SEO.Description)),
		NoIndex:     result.SEO.NoIndexValue(),
	}
	document.Content, document.Links = siteSearchContent(result.HTML)
	return document
}

func writeSiteSearchDocument(w http.ResponseWriter, status int, r *http.Request, result RenderResult) {
	document := newSiteSearchDocument(r.URL.Path, result)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", siteSearchHeader)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(document)
}

func (a *App) siteSearchStreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	queue := append([]string(nil), a.pages.Routes()...)
	for name := range a.content {
		queue = append(queue, "/post/"+name)
	}
	seen := make(map[string]struct{}, len(queue))
	for _, path := range queue {
		seen[path] = struct{}{}
	}
	limit := a.config.SiteSearch.MaxPages
	if limit < 1 {
		limit = 500
	}
	for index := 0; index < len(queue) && index < limit; index++ {
		path := queue[index]
		request := r.Clone(r.Context())
		request.URL.Path, request.URL.RawQuery = path, ""
		document, ok := a.renderSiteSearchPath(request, path)
		if !ok {
			continue
		}
		if document.NoIndex {
			continue
		}
		_ = encoder.Encode(document)
		if flusher != nil {
			flusher.Flush()
		}
		for _, href := range document.Links {
			url, err := request.URL.Parse(href)
			if err != nil || url.IsAbs() || url.Host != "" || url.Path == "" || url.RawQuery != "" || strings.HasPrefix(url.Path, "/_") || strings.HasPrefix(url.Path, "/api/") {
				continue
			}
			path = CanonicalURL(url.Path)
			if _, exists := seen[path]; !exists && len(queue) < limit {
				seen[path] = struct{}{}
				queue = append(queue, path)
			}
		}
	}
}

func (a *App) renderSiteSearchPath(r *http.Request, path string) (siteSearchDocument, bool) {
	if strings.HasPrefix(path, "/post/") {
		request := r.Clone(r.Context())
		request.Header = r.Header.Clone()
		request.Header.Set(siteSearchHeader, "1")
		capture := &siteSearchResponseCapture{header: make(http.Header)}
		a.contentHandler(capture, request)
		if capture.status != http.StatusOK {
			return siteSearchDocument{}, false
		}
		var document siteSearchDocument
		if json.Unmarshal(capture.body.Bytes(), &document) != nil {
			return siteSearchDocument{}, false
		}
		return document, true
	}
	page, params, status, err := a.selectTenantPage(r)
	if err != nil || page == nil || status != http.StatusOK {
		return siteSearchDocument{}, false
	}
	result, _, err := a.renderPageCached(r, page, params)
	if err != nil {
		return siteSearchDocument{}, false
	}
	return newSiteSearchDocument(path, result), true
}

func (a *App) rebuildSiteSearchIndex() error {
	request, err := http.NewRequest(http.MethodGet, "http://localhost/_gosh/site-search", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-GOSH-Site-Search-Rebuild", "1")
	capture := &siteSearchResponseCapture{header: make(http.Header)}
	a.wrapRequest(http.HandlerFunc(a.siteSearchStreamHandler)).ServeHTTP(capture, request)
	if capture.status != http.StatusOK {
		return fmt.Errorf("site search index: status %d", capture.status)
	}
	var index []siteSearchDocument
	for _, line := range bytes.Split(capture.body.Bytes(), []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var document siteSearchDocument
		if err := json.Unmarshal(line, &document); err != nil {
			return err
		}
		index = append(index, document)
	}
	a.siteSearchMu.Lock()
	a.siteSearchIndex = index
	a.siteSearchMu.Unlock()
	return nil
}

func (a *App) siteSearchQueryHandler(w http.ResponseWriter, r *http.Request) {
	if !a.config.SiteSearch.ServerSearch {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if query == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"count": len(a.siteSearchIndex), "results": []any{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	words := strings.Fields(query)
	operator := strings.ToLower(r.URL.Query().Get("operator"))
	locale := strings.ToLower(strings.Split(r.URL.Query().Get("locale"), "-")[0])
	a.siteSearchMu.RLock()
	index := append([]siteSearchDocument(nil), a.siteSearchIndex...)
	a.siteSearchMu.RUnlock()
	results := make([]map[string]any, 0, limit)
	for _, document := range index {
		text := strings.ToLower(document.Title + " " + document.Description + " " + document.Content)
		matches := 0
		for _, word := range words {
			if strings.Contains(text, word) {
				matches++
			}
		}
		if (operator == "and" && matches != len(words)) || (operator != "and" && matches == 0) {
			continue
		}
		if locale != "" && siteSearchDocumentLocale(document.Path, a.config) != locale && hasLocalizedSibling(index, document.Path, locale, a.config) {
			continue
		}
		snippet := serverSearchSnippet(document, words)
		results = append(results, map[string]any{"id": document.Path, "url": document.Path, "path": document.Path, "title": document.Title, "description": document.Description, "snippet": snippet, "locale": siteSearchDocumentLocale(document.Path, a.config), "score": float64(1) / float64(matches)})
		if len(results) == limit {
			break
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
}

func serverSearchSnippet(document siteSearchDocument, words []string) string {
	for _, source := range []string{document.Content, document.Description, document.Title} {
		lower := strings.ToLower(source)
		for _, word := range words {
			if at := strings.Index(lower, word); at >= 0 {
				start, end := at-80, at+len(word)+180
				if start < 0 {
					start = 0
				}
				if end > len(source) {
					end = len(source)
				}
				for start > 0 && !utf8.RuneStart(source[start]) {
					start--
				}
				for end < len(source) && !utf8.RuneStart(source[end]) {
					end--
				}
				if start > 0 {
					for start < len(source) {
						r, size := utf8.DecodeRuneInString(source[start:])
						if unicode.IsSpace(r) {
							start += size
							break
						}
						start += size
					}
				}
				if end < len(source) {
					for end > 0 {
						previous := end - 1
						for previous > 0 && !utf8.RuneStart(source[previous]) {
							previous--
						}
						r, _ := utf8.DecodeRuneInString(source[previous:end])
						if unicode.IsSpace(r) {
							break
						}
						end = previous
					}
				}
				prefix, suffix := "", ""
				if start > 0 {
					prefix = "…"
				}
				if end < len(source) {
					suffix = "…"
				}
				return prefix + source[start:end] + suffix
			}
		}
	}
	return document.Content
}

func siteSearchDocumentLocale(path string, config logic.RuntimeConfig) string {
	part := strings.ToLower(strings.Split(strings.Trim(path, "/"), "/")[0])
	for _, locale := range config.Locales {
		if strings.ToLower(strings.Split(locale, "-")[0]) == part {
			return part
		}
	}
	return strings.ToLower(strings.Split(config.DefaultLocale, "-")[0])
}

func hasLocalizedSibling(index []siteSearchDocument, path, locale string, config logic.RuntimeConfig) bool {
	base := "/" + strings.Join(strings.Split(strings.Trim(path, "/"), "/")[1:], "/")
	for _, document := range index {
		if siteSearchDocumentLocale(document.Path, config) == locale && strings.TrimPrefix(document.Path, "/"+locale) == base {
			return true
		}
	}
	return false
}

func buildSiteSearchIndex(output string) error {
	config, err := logic.UseRuntimeConfig()
	if err != nil {
		return err
	}
	if !config.SiteSearch.Enabled || config.SiteSearch.ServerSearch {
		return nil
	}
	app, err := newAppWithLogger(log.New(io.Discard, "", 0))
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodGet, "http://localhost/_gosh/site-search", nil)
	if err != nil {
		return err
	}
	capture := &siteSearchResponseCapture{header: make(http.Header)}
	app.wrapRequest(http.HandlerFunc(app.siteSearchStreamHandler)).ServeHTTP(capture, request)
	if capture.status != http.StatusOK {
		return fmt.Errorf("build site search index: status %d", capture.status)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, capture.body.Bytes(), 0o644)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func siteSearchContent(source string) (string, []string) {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", nil
	}
	main := findHTMLElement(root, "main")
	if main == nil {
		main = root
	}
	var text strings.Builder
	links := make([]string, 0)
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, hidden bool) {
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if tag == "script" || tag == "style" || tag == "noscript" || tag == "template" || tag == "svg" || tag == "header" || tag == "footer" || tag == "nav" || tag == "form" || tag == "dialog" {
				return
			}
			for _, attr := range node.Attr {
				if attr.Key == "data-nosnippet" || (attr.Key == "aria-hidden" && strings.EqualFold(attr.Val, "true")) {
					hidden = true
				}
				if tag == "a" && attr.Key == "href" && !hidden {
					links = append(links, attr.Val)
				}
			}
		}
		if node.Type == html.TextNode && !hidden {
			text.WriteString(node.Data)
			text.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, hidden)
		}
	}
	walk(main, false)
	return strings.Join(strings.Fields(text.String()), " "), links
}

func findHTMLElement(node *html.Node, tag string) *html.Node {
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}
