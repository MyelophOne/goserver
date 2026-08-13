package goserver

import (
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

//go:embed base.html
var embeddedBase []byte

const TemplatesDir = "./templates"

type PageData struct {
	Content     any
	Variables   map[string]any
	Title       string
	Description string

	GlobalStyles  template.HTML
	PageStyles    template.HTML
	GlobalScripts template.HTML
	PageScripts   template.HTML
	GlobalHead    template.HTML
	PageHead      template.HTML
}

type TemplateManager struct {
	templates map[string]*template.Template
	layouts   map[string]string
	assets    map[string]PageData

	GlobalScripts template.HTML
	GlobalStyles  template.HTML
	GlobalHead    template.HTML
	mu            sync.RWMutex
}

func NewTemplateManager(defaultLayout ...string) *TemplateManager {
	layout := "base.html"
	if len(defaultLayout) > 0 && defaultLayout[0] != "" {
		layout = defaultLayout[0]
	}

	m := &TemplateManager{
		templates: make(map[string]*template.Template),
		layouts:   make(map[string]string),
		assets:    make(map[string]PageData),
	}
	m.loadTemplates(layout)
	m.loadAssets()
	return m
}

func (m *TemplateManager) loadTemplates(defaultLayout string) {
	partialFiles, _ := filepath.Glob(filepath.Join(TemplatesDir, "partials", "*.html"))
	pages, _ := filepath.Glob(filepath.Join(TemplatesDir, "pages", "*.html"))
	if len(pages) == 0 {
		return
	}

	funcMap := template.FuncMap{
		"default": DefaultRenderer,
		"var":     GetTemplateVar,
	}

	for _, page := range pages {
		pageName := filepath.Base(page)
		layout := defaultLayout

		if strings.Contains(pageName, "@") {
			parts := strings.SplitN(pageName, "@", 2)
			pageName = parts[0] + ".html"
			layout = strings.TrimSuffix(parts[1], ".html") + ".html"
		}

		layoutPath := filepath.Join(TemplatesDir, "layouts", layout)
		var tmpl *template.Template
		var err error

		if _, errStat := os.Stat(layoutPath); errStat == nil {
			files := append([]string{layoutPath, page}, partialFiles...)
			tmpl, err = template.New("").Funcs(funcMap).ParseFiles(files...)
		} else {
			tmpl, err = template.New("").Funcs(funcMap).
				Parse(string(embeddedBase))
			if err == nil {
				_, err = tmpl.ParseFiles(append([]string{page}, partialFiles...)...)
			}
		}

		if err != nil {
			log.Printf("[templates] error parsing %s: %v", page, err)
			continue
		}

		m.mu.Lock()
		m.templates[pageName] = tmpl
		m.layouts[pageName] = strings.TrimSuffix(layout, ".html")
		m.mu.Unlock()
	}
}

func (m *TemplateManager) loadAssets() {
	if content, err := os.ReadFile(filepath.Join(TemplatesDir, "scripts", "global.html")); err == nil {
		m.GlobalScripts = template.HTML(content)
	}
	if content, err := os.ReadFile(filepath.Join(TemplatesDir, "styles", "global.html")); err == nil {
		m.GlobalStyles = template.HTML(content)
	}
	if content, err := os.ReadFile(filepath.Join(TemplatesDir, "head", "global.html")); err == nil {
		m.GlobalHead = template.HTML(content)
	}

	subdirs := []string{"head", "styles", "scripts"}
	for _, dir := range subdirs {
		files, _ := filepath.Glob(filepath.Join(TemplatesDir, dir, "*.html"))
		for _, f := range files {
			page := filepath.Base(f)
			content, err := os.ReadFile(f)
			if err != nil {
				log.Printf("[templates] error reading %s: %v", f, err)
				continue
			}

			m.mu.Lock()
			asset := m.assets[page]
			switch dir {
			case "head":
				asset.PageHead = template.HTML(content)
			case "styles":
				asset.PageStyles = template.HTML(content)
			case "scripts":
				asset.PageScripts = template.HTML(content)
			}
			m.assets[page] = asset
			m.mu.Unlock()
		}
	}
}

func (m *TemplateManager) RenderHTML(page string, data PageData) (string, error) {
	m.mu.RLock()
	tmpl, ok := m.templates[page]
	layout := m.layouts[page]
	assets := m.assets[page]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("template %s not found", page)
	}

	if assets.PageHead != "" {
		data.PageHead = assets.PageHead
	}
	if assets.PageStyles != "" {
		data.PageStyles = assets.PageStyles
	}
	if assets.PageScripts != "" {
		data.PageScripts = assets.PageScripts
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, layout, data); err != nil {
		return "", err
	}

	oneLine := strings.ReplaceAll(buf.String(), "\n", "")
	oneLine = strings.ReplaceAll(oneLine, "\t", "")
	oneLine = strings.Join(strings.Fields(oneLine), " ")

	return oneLine, nil
}

func DefaultRenderer(def string, val any) string {
	if val == nil {
		return strings.TrimSpace(def)
	}
	switch v := val.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return strings.TrimSpace(def)
		}
		return v
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func GetTemplateVar(data any, key string) any {
	if pd, ok := data.(PageData); ok {
		if pd.Variables != nil {
			if val, ok := pd.Variables[key]; ok {
				return val
			}
		}

		v := reflect.ValueOf(pd)
		f := v.FieldByNameFunc(func(name string) bool {
			return strings.EqualFold(name, key)
		})
		if f.IsValid() {
			return f.Interface()
		}
	}

	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Map {
		mapVal := val.MapIndex(reflect.ValueOf(key))
		if mapVal.IsValid() {
			return mapVal.Interface()
		}
	}

	return nil
}

func RenderHTMLString(tplString string, data any) (string, error) {
	t, err := template.New("dynamic_tpl").Parse(tplString)
	if err != nil {
		return "", fmt.Errorf("template parsing error: %w", err)
	}

	var builder strings.Builder

	err = t.Execute(&builder, data)
	if err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}

	return builder.String(), nil
}
