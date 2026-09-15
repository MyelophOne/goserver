package logic

import runtime "github.com/myelophone/goserver/web/runtime"

type (
	Context                  = runtime.Context
	Props                    = runtime.Props
	Data                     = runtime.Data
	Handler                  = runtime.Handler
	Noop                     = runtime.Noop
	SEOInput                 = runtime.SEOInput
	SEOState                 = runtime.SEOState
	FullscreenPreloaderInput = runtime.FullscreenPreloaderInput
	FullscreenPreloaderState = runtime.FullscreenPreloaderState
	RuntimeConfig            = runtime.RuntimeConfig
	RouteRule                = runtime.RouteRule
	CachePolicy              = runtime.CachePolicy
	Module                   = runtime.Module
)

var (
	UseSeo                    = runtime.UseSeo
	SEOText                   = runtime.SEOText
	SEOFlag                   = runtime.SEOFlag
	UseFullscreenPreloader    = runtime.UseFullscreenPreloader
	PreloaderFlag             = runtime.PreloaderFlag
	PreloaderInt              = runtime.PreloaderInt
	UseStoreState             = runtime.UseStoreState
	StoreState                = runtime.StoreState
	UseCacheTags              = runtime.UseCacheTags
	CacheTags                 = runtime.CacheTags
	RevalidateTags            = runtime.RevalidateTags
	RevalidationTags          = runtime.RevalidationTags
	MustRuntimeConfig         = runtime.MustRuntimeConfig
	UseRuntimeConfig          = runtime.UseRuntimeConfig
	CloneContextForBackground = runtime.CloneContextForBackground
	RegisterModule            = runtime.RegisterModule
)
