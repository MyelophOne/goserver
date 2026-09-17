package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type CachePolicy struct {
	MaxAge int `json:"maxAge"`
}
type RouteRule struct {
	SWR          int          `json:"swr"`
	Cache        *CachePolicy `json:"cache,omitempty"`
	PublicStatic bool         `json:"publicStatic,omitempty"`
	Exclude      []string     `json:"exclude,omitempty"`
}

type CookieScriptLegalInfo struct {
	Purpose            string `json:"purpose,omitempty"`
	CookiesUsed        *bool  `json:"cookiesUsed,omitempty"`
	CookiesDescription string `json:"cookiesDescription,omitempty"`
	Retention          string `json:"retention,omitempty"`
	PrivacyPolicyURL   string `json:"privacyPolicyUrl,omitempty"`
	OfficialDocsURL    string `json:"officialDocsUrl,omitempty"`
	Notes              string `json:"notes,omitempty"`
}

type CookieScriptInitializer struct {
	Key  string `json:"key"`
	Code string `json:"code"`
}

type CookieScriptCategorySettings struct {
	Initializers    []CookieScriptInitializer `json:"initializers,omitempty"`
	BeforeLoad      string                    `json:"beforeLoad,omitempty"`
	OnConsentChange string                    `json:"onConsentChange,omitempty"`
}

type CookieScript struct {
	ID               string                                  `json:"id"`
	Name             string                                  `json:"name"`
	Description      string                                  `json:"description,omitempty"`
	Provider         string                                  `json:"provider,omitempty"`
	LegalBasis       string                                  `json:"legalBasis,omitempty"`
	Legal            CookieScriptLegalInfo                   `json:"legal,omitempty"`
	LoadKey          string                                  `json:"loadKey,omitempty"`
	Src              string                                  `json:"src,omitempty"`
	Code             string                                  `json:"code,omitempty"`
	Initializers     []CookieScriptInitializer               `json:"initializers,omitempty"`
	BeforeLoad       string                                  `json:"beforeLoad,omitempty"`
	OnConsentChange  string                                  `json:"onConsentChange,omitempty"`
	CategorySettings map[string]CookieScriptCategorySettings `json:"categorySettings,omitempty"`
	Attributes       map[string]string                       `json:"attributes,omitempty"`
}

type CookieBannerConfig struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Position string `json:"position,omitempty"`
}

type CookieSettingsConfig struct {
	Enabled        *bool `json:"enabled,omitempty"`
	ShowCookieList *bool `json:"showCookieList,omitempty"`
}

type CookieControlConfig struct {
	Enabled          bool                 `json:"enabled"`
	AutoMount        *bool                `json:"autoMount,omitempty"`
	CookieName       string               `json:"cookieName,omitempty"`
	MaxAgeDays       int                  `json:"maxAgeDays,omitempty"`
	DeclineReaskDays int                  `json:"declineReaskDays,omitempty"`
	Banner           CookieBannerConfig   `json:"banner,omitempty"`
	Settings         CookieSettingsConfig `json:"settings,omitempty"`
}

type RuntimeConfig struct {
	Environment string `json:"-"`
	Runtime     struct {
		Enabled               bool `json:"enabled"`
		CacheTTL              int  `json:"cacheTTL"`
		WebVitals             bool `json:"webVitals"`
		PrefetchDelay         int  `json:"prefetchDelay"`
		PrefetchMaxConcurrent int  `json:"prefetchMaxConcurrent"`
		PrefetchOnHover       bool `json:"prefetchOnHover"`
		ViewTransitions       bool `json:"viewTransitions"`
	} `json:"runtime"`
	Render struct {
		DefaultLayout      string `json:"defaultLayout"`
		EarlyHints         bool   `json:"earlyHints"`
		ServerTiming       bool   `json:"serverTiming"`
		PreloadRuntime     bool   `json:"preloadRuntime"`
		PreloadPageStyles  bool   `json:"preloadPageStyles"`
		SplitCSS           bool   `json:"splitCss"`
		CSSMinChunkSize    int    `json:"cssMinChunkSize"`
		CSSMaxChunkSize    int    `json:"cssMaxChunkSize"`
		SPALoadingTemplate bool   `json:"spaLoadingTemplate"`
	} `json:"render"`
	Tailwind struct {
		Minify bool `json:"minify"`
	} `json:"tailwind"`
	SiteSearch struct {
		Enabled  bool `json:"enabled"`
		Shortcut struct {
			Key        string `json:"key"`
			CtrlOrMeta bool   `json:"ctrlOrMeta"`
			Shift      bool   `json:"shift"`
		} `json:"shortcut"`
		MinQueryLength int  `json:"minQueryLength"`
		Limit          int  `json:"limit"`
		MaxPages       int  `json:"maxPages"`
		CacheTTL       int  `json:"cacheTtl"`
		ServerSearch   bool `json:"serverSearch"`
	} `json:"siteSearch"`
	QuickCommands struct {
		Enabled  bool `json:"enabled"`
		Shortcut struct {
			Key        string `json:"key"`
			CtrlOrMeta bool   `json:"ctrlOrMeta"`
			Shift      bool   `json:"shift"`
		} `json:"shortcut"`
		Items []map[string]any `json:"items"`
	} `json:"quickCommands"`
	Content struct {
		Layout string `json:"layout"`
	} `json:"content"`
	Preloader      FullscreenPreloaderInput  `json:"preloader"`
	CookieControl  CookieControlConfig       `json:"cookieControl"`
	CookieScripts  map[string][]CookieScript `json:"cookieScripts"`
	RouteRules     map[string]RouteRule      `json:"routeRules"`
	ComponentRules map[string]RouteRule      `json:"componentRules"`
	Images         struct {
		Optimize    bool `json:"optimize"`
		JPEGQuality int  `json:"jpegQuality"`
	} `json:"images"`
	Build struct {
		IncludeFiles []string `json:"includeFiles"`
	} `json:"build"`
	Locales       []string       `json:"locales"`
	DefaultLocale string         `json:"defaultLocale"`
	SEO           SEOInput       `json:"seo"`
	Values        map[string]any `json:"values"`
}

func defaultConfig() RuntimeConfig {
	var c RuntimeConfig
	c.Environment = "Development"
	c.Runtime.PrefetchDelay, c.Runtime.PrefetchMaxConcurrent, c.Runtime.PrefetchOnHover, c.Runtime.ViewTransitions = 65, 2, false, true
	c.Render.DefaultLayout, c.Render.EarlyHints, c.Render.PreloadRuntime, c.Render.PreloadPageStyles, c.Render.SplitCSS = "default", true, true, true, true
	c.Render.CSSMinChunkSize, c.Render.CSSMaxChunkSize = 4096, 65536
	c.Tailwind.Minify, c.Images.Optimize, c.Images.JPEGQuality = true, true, 82
	c.SiteSearch.MinQueryLength, c.SiteSearch.Limit, c.SiteSearch.MaxPages, c.SiteSearch.CacheTTL = 2, 10, 500, 1800000
	c.CookieControl.Enabled = true
	c.CookieControl.CookieName = "privacy-preferences"
	c.CookieControl.MaxAgeDays, c.CookieControl.DeclineReaskDays = 365, 30
	c.CookieScripts = map[string][]CookieScript{"necessary": {}, "analytics": {}, "marketing": {}, "functional": {}, "notices": {}}
	c.RouteRules, c.ComponentRules, c.Values = map[string]RouteRule{}, map[string]RouteRule{}, map[string]any{}
	c.SEO.Image = "/assets/seo/goserver-cat.jpg"
	return c
}

var configOnce sync.Once
var runtimeConfig RuntimeConfig
var configErr error

func mergeObject(dst, src map[string]any) {
	for key, value := range src {
		if source, ok := value.(map[string]any); ok {
			if target, ok := dst[key].(map[string]any); ok {
				mergeObject(target, source)
				continue
			}
		}
		dst[key] = value
	}
}

func mergeConfig(path string, cfg *RuntimeConfig) error {
	data, err := configFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	base, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var target, patch map[string]any
	if err = json.Unmarshal(base, &target); err != nil {
		return err
	}
	if err = json.Unmarshal(data, &patch); err != nil {
		return err
	}
	mergeObject(target, patch)
	merged, err := json.Marshal(target)
	if err != nil {
		return err
	}
	return json.Unmarshal(merged, cfg)
}

func UseRuntimeConfig() (RuntimeConfig, error) {
	configOnce.Do(func() {
		runtimeConfig = defaultConfig()
		runtimeConfig.Environment = environment()
		if configErr = mergeConfig("websettings.json", &runtimeConfig); configErr != nil {
			return
		}
		configErr = mergeConfig(filepath.Join("websettings."+runtimeConfig.Environment+".json"), &runtimeConfig)
		if value, ok := os.LookupEnv("MYELOPHONE_WEB_RUNTIME_WEB_VITALS"); ok {
			runtimeConfig.Runtime.WebVitals, configErr = strconv.ParseBool(value)
		}
		if value, ok := os.LookupEnv("MYELOPHONE_WEB_RENDER_SERVER_TIMING"); ok && configErr == nil {
			runtimeConfig.Render.ServerTiming, configErr = strconv.ParseBool(value)
		}
		if value, ok := os.LookupEnv("MYELOPHONE_WEB_ENABLED"); ok && configErr == nil {
			runtimeConfig.Runtime.Enabled, configErr = strconv.ParseBool(value)
		}
	})
	return runtimeConfig, configErr
}

func MustRuntimeConfig() RuntimeConfig            { c, _ := UseRuntimeConfig(); return c }
func (c RuntimeConfig) Value(name string) any     { return c.Values[name] }
func (c RuntimeConfig) String(name string) string { v, _ := c.Values[name].(string); return v }
func (c RuntimeConfig) Bool(name string) bool     { v, _ := c.Values[name].(bool); return v }

type HookPayload struct {
	Name, Key string
	App       *AppContext
	Data      any
	Error     error
	Values    map[string]any
}
type Hook func(*HookPayload) error

const (
	HookBuildBefore           = "build:before"
	HookBuildGraph            = "build:graph"
	HookBuildClientBefore     = "build:client:before"
	HookBuildClientAfter      = "build:client:after"
	HookBuildTailwindBefore   = "build:tailwind:before"
	HookBuildTailwindAfter    = "build:tailwind:after"
	HookBuildAssetsBefore     = "build:assets:before"
	HookBuildAssetsAfter      = "build:assets:after"
	HookBuildPublicBefore     = "build:public:before"
	HookBuildPublicAfter      = "build:public:after"
	HookBuildDone             = "build:done"
	HookServerRequestBefore   = "server:request:before"
	HookServerRequestAfter    = "server:request:after"
	HookServerRenderBefore    = "server:render:before"
	HookServerRenderAfter     = "server:render:after"
	HookServerComponentBefore = "server:component:before"
	HookServerComponentAfter  = "server:component:after"
	HookServerActionBefore    = "server:action:before"
	HookServerActionAfter     = "server:action:after"
	HookServerCacheHit        = "server:cache:hit"
	HookServerCacheMiss       = "server:cache:miss"
	HookServerCacheStale      = "server:cache:stale"
	HookServerError           = "server:error"
)

func CallHook(string, *HookPayload) error { return nil }

type Module interface {
	Name() string
	Setup(*ModuleContext) error
}
type ModuleContext struct{ Config RuntimeConfig }

var registeredModules = struct {
	sync.RWMutex
	items []Module
}{}

func RegisterModule(module Module) {
	if module == nil {
		return
	}
	registeredModules.Lock()
	registeredModules.items = append(registeredModules.items, module)
	registeredModules.Unlock()
}

var modulesOnce sync.Once
var modulesErr error

func SetupModules(config RuntimeConfig) error {
	modulesOnce.Do(func() {
		registeredModules.RLock()
		items := append([]Module(nil), registeredModules.items...)
		registeredModules.RUnlock()
		for _, module := range items {
			if err := module.Setup(&ModuleContext{Config: config}); err != nil {
				modulesErr = err
				return
			}
		}
	})
	return modulesErr
}
func SetupRequestPlugins(*AppContext) error { return nil }

type AppContext struct {
	Event  *RequestEvent
	Config RuntimeConfig
	Error  error
}
type RequestEvent struct {
	Writer  http.ResponseWriter
	Request *http.Request
}

func EnterRequest(w http.ResponseWriter, r *http.Request, c RuntimeConfig) (*AppContext, func()) {
	return &AppContext{Event: &RequestEvent{Writer: w, Request: r}, Config: c}, func() {}
}
func UseApp() *AppContext                { return nil }
func BindBackgroundApp(fn func()) func() { return fn }
func CloneContextForBackground(c *Context) *Context {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}
func CachedComponentData(_ string, _ string, _ Props, _ RouteRule, fn func() (Data, error), _ ...func() (Data, error)) (Data, string, error) {
	d, e := fn()
	return d, "bypass", e
}
func SetError(error) {}

type HTTPError struct {
	StatusCode    int
	StatusMessage string
	Data          any
	Cause         error
}

func (e *HTTPError) Error() string {
	if e != nil && e.StatusMessage != "" {
		return e.StatusMessage
	}
	return "internal server error"
}
func ErrorStatus(err error) int {
	var e *HTTPError
	if errors.As(err, &e) && e.StatusCode != 0 {
		return e.StatusCode
	}
	return http.StatusInternalServerError
}

type BuildAsset struct {
	Path              string
	RawSize, GzipSize int
}
type Server interface {
	RespondJSON(http.ResponseWriter, *http.Request, any)
	RespondHTML(http.ResponseWriter, string)
	RespondText(http.ResponseWriter, string)
	RespondRawJSON(http.ResponseWriter, []byte)
	RespondSecureJSON(http.ResponseWriter, *http.Request, any)
	RespondXML(http.ResponseWriter, *http.Request, any)
	RespondAsciiJSON(http.ResponseWriter, *http.Request, any)
}
type APIHandler func(Server, http.ResponseWriter, *http.Request)
type Route struct {
	Method, Path string
	Handler      APIHandler
}

func Routes() []Route { return nil }

var handlers = struct {
	sync.RWMutex
	items map[string]Handler
}{items: map[string]Handler{}}

func RegisterHandler(export string, handler Handler) {
	handlers.Lock()
	handlers.items[export] = handler
	handlers.Unlock()
}

func Resolve(export string) (Handler, bool) {
	handlers.RLock()
	handler, ok := handlers.items[export]
	handlers.RUnlock()
	return handler, ok
}

type Event struct {
	Writer  http.ResponseWriter
	Request *http.Request
	Params  map[string]string
}

func (e *Event) Context() context.Context {
	if e == nil || e.Request == nil {
		return context.Background()
	}
	return e.Request.Context()
}
func (e *Event) Param(name string) string {
	if e == nil {
		return ""
	}
	return e.Params[name]
}
func (e *Event) Query(name string) string {
	if e == nil || e.Request == nil {
		return ""
	}
	return e.Request.URL.Query().Get(name)
}
func (e *Event) Header(name string) string {
	if e == nil || e.Request == nil {
		return ""
	}
	return e.Request.Header.Get(name)
}
func (e *Event) Status(code int) *Event {
	if e != nil && e.Writer != nil {
		e.Writer.WriteHeader(code)
	}
	return e
}
func (e *Event) JSON(value any) error {
	if e == nil || e.Writer == nil {
		return nil
	}
	e.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(e.Writer).Encode(value)
}
func (e *Event) Text(value string) error {
	if e == nil || e.Writer == nil {
		return nil
	}
	e.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, err := e.Writer.Write([]byte(value))
	return err
}
func (e *Event) HTML(value string) error {
	if e == nil || e.Writer == nil {
		return nil
	}
	e.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := e.Writer.Write([]byte(value))
	return err
}

type ServerHandler func(*Event) error
type ServerRoute struct {
	Method, Path string
	Handler      ServerHandler
}

var serverRoutes = struct {
	sync.RWMutex
	items []ServerRoute
}{}

func RegisterServerRoute(route ServerRoute) {
	if route.Method == "" || route.Path == "" || route.Handler == nil {
		return
	}
	serverRoutes.Lock()
	serverRoutes.items = append(serverRoutes.items, route)
	serverRoutes.Unlock()
}
func ServerRoutes() []ServerRoute {
	serverRoutes.RLock()
	out := append([]ServerRoute(nil), serverRoutes.items...)
	serverRoutes.RUnlock()
	return out
}

var _ = context.Background
var _ = os.Getenv
var _ = time.Now
