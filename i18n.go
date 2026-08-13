package goserver

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

const langKey ctxKey = "lang"

//go:embed i18n/**
var internalI18n embed.FS

var registeredExternalI18n []fs.FS

func (s *Server) RegisterI18nFS(fsys fs.FS) {
	if fsys != nil {
		registeredExternalI18n = append(registeredExternalI18n, fsys)
	}
}

type I18n struct {
	messages    map[string]map[string]string
	defaultLang string
}

func (s *Server) NewI18n(
	defaultLang string,
	supportedLangs []string,
) (*I18n, error) {

	i := &I18n{
		messages:    make(map[string]map[string]string),
		defaultLang: defaultLang,
	}

	for _, lang := range supportedLangs {
		i.messages[lang] = make(map[string]string)
	}

	if err := i.loadFromFS(internalI18n); err != nil {
		return nil, err
	}

	for _, fsys := range registeredExternalI18n {
		if err := i.loadFromFS(fsys); err != nil {
			return nil, err
		}
	}

	s.I18n = i

	s.Use(s.I18nMiddleware(s.I18n))

	return i, nil
}

func (i *I18n) loadFromFS(fsys fs.FS) error {
	return fs.WalkDir(fsys, "i18n", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		relPath, _ := filepath.Rel("i18n", path)
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) < 2 {
			return nil
		}

		lang := parts[0]
		filename := parts[1]
		namespace := strings.TrimSuffix(filename, ".json")

		if _, ok := i.messages[lang]; !ok {
			i.messages[lang] = make(map[string]string)
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		var rawContent any
		if err := json.Unmarshal(data, &rawContent); err != nil {
			return fmt.Errorf("error parsing %s: %w", path, err)
		}

		flattenMap(namespace, rawContent, i.messages[lang])
		return nil
	})
}

func flattenMap(prefix string, data any, result map[string]string) {
	switch v := data.(type) {
	case map[string]any:
		for k, val := range v {
			newKey := k
			if prefix != "" {
				newKey = prefix + "." + k
			}
			flattenMap(newKey, val, result)
		}
	case string:
		result[prefix] = v
	case float64:
		result[prefix] = strconv.FormatFloat(v, 'f', -1, 64)
	}
}

func (i *I18n) L(ctx context.Context, key string) string {
	lang, _ := ctx.Value(langKey).(string)
	if lang == "" {
		lang = i.defaultLang
	}
	return i.Lang(key, lang)
}

func (i *I18n) Lang(key, lang string) string {
	if val := i.lookup(lang, key); val != "" {
		return val
	}
	if val := i.lookup(i.defaultLang, key); val != "" {
		return val
	}
	return key
}

func (i *I18n) Lf(ctx context.Context, key string, params map[string]string) string {
	lang, _ := ctx.Value(langKey).(string)
	if lang == "" {
		lang = i.defaultLang
	}
	return i.Langf(key, lang, params)
}

func (i *I18n) Langf(key, lang string, params map[string]string) string {
	str := i.Lang(key, lang)
	if params == nil {
		return str
	}
	for k, v := range params {
		str = strings.ReplaceAll(str, "{"+k+"}", v)
	}
	return str
}

func (i *I18n) Pluralf(ctx context.Context, key string, count int, params map[string]string) string {
	lang, _ := ctx.Value(langKey).(string)
	if lang == "" {
		lang = i.defaultLang
	}
	return i.PluralLangf(key, lang, count, params)
}

func (i *I18n) PluralLangf(key, lang string, count int, params map[string]string) string {
	if count == 0 {
		zeroKey := key + ".zero"
		if val := i.lookup(lang, zeroKey); val != "" {
			return i.finalizeString(val, count, params)
		}
	}

	form := pluralForm(lang, count)
	fullKey := key + "." + form

	if i.lookup(lang, fullKey) == "" {
		fullKey = key + ".other"
	}

	str := i.Lang(fullKey, lang)
	return i.finalizeString(str, count, params)
}

func (i *I18n) finalizeString(tmpl string, count int, params map[string]string) string {
	if params == nil {
		params = make(map[string]string)
	}
	params["count"] = strconv.Itoa(count)

	for k, v := range params {
		tmpl = strings.ReplaceAll(tmpl, "{"+k+"}", v)
	}
	return tmpl
}

func (i *I18n) lookup(lang, key string) string {
	if msgs, ok := i.messages[lang]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	return ""
}

func pluralForm(lang string, n int) string {
	if n < 0 {
		n = -n
	}

	switch lang {
	case "ru", "uk", "be":
		mod10 := n % 10
		mod100 := n % 100

		if mod10 == 1 && mod100 != 11 {
			return "one"
		}
		if mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20) {
			return "few"
		}
		return "many"

	default:
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

func (s *Server) I18nMiddleware(i18n *I18n) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if v := r.Context().Value(langKey); v != nil {
				if l, ok := v.(string); ok && l != "" {
					next.ServeHTTP(w, r)
					return
				}
			}

			lang := i18n.defaultLang

			path := strings.Trim(r.URL.Path, "/")
			parts := strings.Split(path, "/")

			if len(parts) > 0 && parts[0] != "" {
				candidate := strings.ToLower(parts[0])

				if i18n.HasLang(candidate) {
					lang = candidate
				}
			}

			if l := r.URL.Query().Get("hl"); l != "" {
				lang = l
			}

			ctx := context.WithValue(r.Context(), langKey, lang)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (s *Server) GetLang(ctx context.Context) string {
	if v := ctx.Value(langKey); v != nil {
		if lang, ok := v.(string); ok && lang != "" {
			return lang
		}
	}
	return s.I18n.defaultLang
}

func (i *I18n) HasLang(lang string) bool {
	_, ok := i.messages[lang]
	return ok
}

func (i *I18n) ShortPluralf(ctx context.Context, key string, count int, params map[string]string) string {
	lang, _ := ctx.Value(langKey).(string)
	if lang == "" {
		lang = i.defaultLang
	}
	return i.ShortPluralLangf(key, lang, count, params)
}

func (i *I18n) ShortPluralLangf(key, lang string, count int, params map[string]string) string {
	shortCountStr := i.FormatShortCount(count, lang)

	if count == 0 {
		zeroKey := key + ".zero"
		if val := i.lookup(lang, zeroKey); val != "" {
			return i.finalizeShortString(val, shortCountStr, params)
		}
	}

	form := pluralForm(lang, count)
	fullKey := key + "." + form

	if i.lookup(lang, fullKey) == "" {
		fullKey = key + ".other"
	}

	str := i.Lang(fullKey, lang)
	return i.finalizeShortString(str, shortCountStr, params)
}

func (i *I18n) finalizeShortString(tmpl string, countStr string, params map[string]string) string {
	if params == nil {
		params = make(map[string]string)
	}

	params["count"] = countStr

	for k, v := range params {
		tmpl = strings.ReplaceAll(tmpl, "{"+k+"}", v)
	}
	return tmpl
}

func (i *I18n) FormatShortCount(count int, lang string) string {
	absCount := count
	if absCount < 0 {
		absCount = -absCount
	}

	if absCount < 1000 {
		return strconv.Itoa(count)
	}

	var val float64
	var suffix string

	switch {
	case absCount >= 1_000_000_000_000:
		val = float64(absCount) / 1_000_000_000_000.0
		suffix = i.lookup(lang, "common.numbers.trillion")
		if suffix == "" {
			suffix = "T"
		}

	case absCount >= 1_000_000_000:
		val = float64(absCount) / 1_000_000_000.0
		suffix = i.lookup(lang, "common.numbers.billion")
		if suffix == "" {
			suffix = "B"
		}

	case absCount >= 1_000_000:
		val = float64(absCount) / 1_000_000.0
		suffix = i.lookup(lang, "common.numbers.million")
		if suffix == "" {
			suffix = "M"
		}

	default:
		val = float64(absCount) / 1_000.0
		suffix = i.lookup(lang, "common.numbers.thousand")
		if suffix == "" {
			suffix = "K"
		}
	}

	str := fmt.Sprintf("%.1f", val)
	str = strings.TrimSuffix(str, ".0")

	if lang == "ru" || lang == "uk" || lang == "be" || lang == "de" || lang == "pl" {
		str = strings.ReplaceAll(str, ".", ",")
	}

	prefix := ""
	if count < 0 {
		prefix = "-"
	}

	return prefix + str + suffix
}
