package runtime

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type Props map[string]any
type Data map[string]any

type SEOInput struct {
	Title          any    `json:"title,omitempty"`
	TitleKey       string `json:"titleKey,omitempty"`
	Params         any    `json:"params,omitempty"`
	Description    any    `json:"description,omitempty"`
	DescriptionKey string `json:"descriptionKey,omitempty"`
	Image          string `json:"image,omitempty"`
	NoIndex        *bool  `json:"noIndex,omitempty"`
}

type SEOState struct {
	SEOInput
	Set bool `json:"-"`
}

type FullscreenPreloaderInput struct {
	Enabled         *bool  `json:"enabled,omitempty"`
	Transparent     *bool  `json:"transparent,omitempty"`
	Background      string `json:"background,omitempty"`
	BackgroundDark  string `json:"backgroundDark,omitempty"`
	ZIndex          *int   `json:"zIndex,omitempty"`
	AriaLabel       string `json:"ariaLabel,omitempty"`
	MinimumDuration *int   `json:"minimumDuration,omitempty"`
}

type FullscreenPreloaderState struct {
	FullscreenPreloaderInput
	Set bool `json:"-"`
}

type Context struct {
	Request        *http.Request
	Path           string
	Params         map[string]any
	ParamPartsMap  map[string][]string
	Translator     func(string) string
	seo            SEOState
	preloader      FullscreenPreloaderState
	stores         map[string]map[string]any
	cacheTags      map[string]struct{}
	revalidateTags map[string]struct{}
}

func (c *Context) T(key string) string {
	if c != nil && c.Translator != nil {
		return c.Translator(key)
	}
	return key
}

func UseSeo(ctx *Context, input SEOInput) {
	if ctx == nil {
		return
	}
	cur := ctx.seo.SEOInput
	if input.Title != nil {
		cur.Title = input.Title
	}
	if input.TitleKey != "" {
		cur.TitleKey = input.TitleKey
	}
	if input.Params != nil {
		cur.Params = input.Params
	}
	if input.Description != nil {
		cur.Description = input.Description
	}
	if input.DescriptionKey != "" {
		cur.DescriptionKey = input.DescriptionKey
	}
	if input.Image != "" {
		cur.Image = input.Image
	}
	if input.NoIndex != nil {
		cur.NoIndex = input.NoIndex
	}
	ctx.seo = SEOState{SEOInput: cur, Set: true}
}

func (c *Context) SEO() SEOState {
	if c == nil {
		return SEOState{}
	}
	return c.seo
}

func (c *Context) SetSEODefaults(input SEOInput) {
	if c == nil {
		return
	}
	c.seo = SEOState{SEOInput: input, Set: false}
}

func PreloaderFlag(value bool) *bool { return &value }
func PreloaderInt(value int) *int    { return &value }

func UseFullscreenPreloader(ctx *Context, input FullscreenPreloaderInput) {
	if ctx == nil {
		return
	}
	current := ctx.preloader.FullscreenPreloaderInput
	if input.Enabled != nil {
		current.Enabled = input.Enabled
	}
	if input.Transparent != nil {
		current.Transparent = input.Transparent
	}
	if input.Background != "" {
		current.Background = input.Background
	}
	if input.BackgroundDark != "" {
		current.BackgroundDark = input.BackgroundDark
	}
	if input.ZIndex != nil {
		current.ZIndex = input.ZIndex
	}
	if input.AriaLabel != "" {
		current.AriaLabel = input.AriaLabel
	}
	if input.MinimumDuration != nil {
		current.MinimumDuration = input.MinimumDuration
	}
	ctx.preloader = FullscreenPreloaderState{FullscreenPreloaderInput: current, Set: true}
}

func (c *Context) SetFullscreenPreloaderDefaults(input FullscreenPreloaderInput) {
	if c != nil {
		c.preloader = FullscreenPreloaderState{FullscreenPreloaderInput: input}
	}
}

func (c *Context) FullscreenPreloader() FullscreenPreloaderState {
	if c == nil {
		return FullscreenPreloaderState{}
	}
	return c.preloader
}

func UseStoreState(c *Context, name string, state map[string]any) map[string]any {
	if c == nil || strings.TrimSpace(name) == "" {
		return map[string]any{}
	}
	if c.stores == nil {
		c.stores = map[string]map[string]any{}
	}
	store := c.stores[name]
	if store == nil {
		store = map[string]any{}
		c.stores[name] = store
	}
	for key, value := range state {
		store[key] = value
	}
	copy := make(map[string]any, len(store))
	for key, value := range store {
		copy[key] = value
	}
	return copy
}

func StoreState(c *Context) map[string]map[string]any {
	if c == nil || len(c.stores) == 0 {
		return nil
	}
	result := make(map[string]map[string]any, len(c.stores))
	for name, state := range c.stores {
		copy := make(map[string]any, len(state))
		for key, value := range state {
			copy[key] = value
		}
		result[name] = copy
	}
	return result
}

func UseCacheTags(c *Context, tags ...string) {
	if c == nil {
		return
	}
	if c.cacheTags == nil {
		c.cacheTags = map[string]struct{}{}
	}
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			c.cacheTags[tag] = struct{}{}
		}
	}
}

func RevalidateTags(c *Context, tags ...string) {
	if c == nil {
		return
	}
	if c.revalidateTags == nil {
		c.revalidateTags = map[string]struct{}{}
	}
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			c.revalidateTags[tag] = struct{}{}
		}
	}
}

func CacheTags(c *Context) []string        { return contextTags(c, false) }
func RevalidationTags(c *Context) []string { return contextTags(c, true) }
func contextTags(c *Context, revalidate bool) []string {
	if c == nil {
		return nil
	}
	set := c.cacheTags
	if revalidate {
		set = c.revalidateTags
	}
	out := make([]string, 0, len(set))
	for tag := range set {
		out = append(out, tag)
	}
	return out
}

func SEOText(parts ...any) string {
	var b strings.Builder
	for _, part := range parts {
		if part == nil {
			continue
		}
		b.WriteString(fmt.Sprint(part))
	}
	return strings.TrimSpace(b.String())
}

func SEOFlag(v bool) *bool { return &v }

func (s SEOState) NoIndexValue() bool {
	return s.NoIndex != nil && *s.NoIndex
}

func (c *Context) Param(name string) string {
	if c == nil {
		return ""
	}
	v, ok := c.Params[name]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (c *Context) ParamParts(name string) []string {
	if c == nil {
		return nil
	}
	if parts, ok := c.ParamPartsMap[name]; ok {
		return append([]string(nil), parts...)
	}
	if v, ok := c.Params[name].([]string); ok {
		return append([]string(nil), v...)
	}
	return nil
}

func (p Props) String(name string) string {
	if v, ok := p[name].(string); ok {
		return v
	}
	return ""
}
func (p Props) Bool(name string) bool {
	if v, ok := p[name].(bool); ok {
		return v
	}
	return false
}
func (p Props) Int(name string) int {
	switch v := p[name].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func (p Props) Form() map[string]any {
	v, _ := p["form"].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(v))
	for key, value := range v {
		out[key] = value
	}
	return out
}

type Handler interface {
	Render(ctx *Context, props Props) (Data, error)
	Action(ctx *Context, action string, props Props) (Data, bool, error)
}

type Noop struct{}

func (Noop) Render(*Context, Props) (Data, error)               { return nil, nil }
func (Noop) Action(*Context, string, Props) (Data, bool, error) { return nil, false, nil }
