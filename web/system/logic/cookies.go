package systemlogic

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	. "github.com/myelophone/goserver/web/runtime"
)

var cookieCategories = map[string]bool{"necessary": true, "analytics": true, "marketing": true, "functional": true}

var defaultCookieTexts = map[string]any{
	"weUseCookies": "We use cookies for site functionality and analytics.", "customize": "Customize", "decline": "Decline", "changePrefs": "Change preferences", "acceptAll": "Accept all",
	"privacyTitle": "Privacy settings", "close": "Close", "cookieNote": "Choose which cookie categories you want to allow.",
	"necessary": "Necessary", "analytics": "Analytics", "marketing": "Marketing", "functional": "Functional",
	"necessaryDesc": "Required for the website to work. Cannot be disabled.", "analyticsDesc": "Helps us understand how visitors use the website.",
	"marketingDesc": "Enables advertising and third-party media.", "functionalDesc": "Enables optional features and embedded services.",
	"alwaysOn": "Always on", "usedServices": "Services used", "cookiesList": "Cookies stored by this browser",
	"emptyList": "No readable cookies found.", "declineAll": "Reject optional", "savePrefs": "Save preferences",
	"contentHidden": "Content hidden", "setPrefs": "Set preferences", "policyTitle": "Cookies and similar technologies",
	"privacyPolicy": "Privacy policy", "officialDocs": "Official documentation",
	"allowCategory": "Allow %s cookies to display this content.", "allowService": "Allow %s cookies to use %s.",
}

type cookieClientConfig struct {
	Control CookieControlConfig       `json:"control"`
	Scripts map[string][]CookieScript `json:"scripts"`
}

var cookieConfigJSONCache struct {
	sync.Once
	value string
}

func cookieConfigJSON() string {
	cookieConfigJSONCache.Do(func() {
		cfg := MustRuntimeConfig()
		control := cfg.CookieControl
		if control.CookieName == "" {
			control.CookieName = "privacy-preferences"
		}
		if control.MaxAgeDays <= 0 {
			control.MaxAgeDays = 365
		}
		if control.DeclineReaskDays < 0 {
			control.DeclineReaskDays = 30
		}
		if control.Banner.Position == "" {
			control.Banner.Position = "bottom-left"
		}
		payload, _ := json.Marshal(cookieClientConfig{Control: control, Scripts: cfg.CookieScripts})
		cookieConfigJSONCache.value = string(payload)
	})
	return cookieConfigJSONCache.value
}

func cookieBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func cookieText(ctx *Context, key, fallback string) string {
	if ctx == nil {
		return fallback
	}
	translationKey := "common.cookies." + key
	value := strings.TrimSpace(ctx.T(translationKey))
	if value == "" || value == translationKey {
		return fallback
	}
	return value
}

func cookieData(ctx *Context) Data {
	cfg := MustRuntimeConfig()
	control := cfg.CookieControl
	if control.CookieName == "" {
		control.CookieName = "privacy-preferences"
	}
	if control.MaxAgeDays <= 0 {
		control.MaxAgeDays = 365
	}
	if control.DeclineReaskDays < 0 {
		control.DeclineReaskDays = 30
	}
	if control.Banner.Position == "" {
		control.Banner.Position = "bottom-left"
	}
	texts := defaultCookieTexts
	if ctx != nil && ctx.Translator != nil {
		texts = make(map[string]any, len(defaultCookieTexts))
		for key, fallback := range defaultCookieTexts {
			texts[key] = cookieText(ctx, key, fallback.(string))
		}
	}
	return Data{
		"cookieControlEnabled":  control.Enabled,
		"cookieAutoMount":       cookieBool(control.AutoMount, true),
		"cookieBannerEnabled":   cookieBool(control.Banner.Enabled, true),
		"cookieSettingsEnabled": cookieBool(control.Settings.Enabled, true),
		"cookieBannerMounted":   control.Enabled && cookieBool(control.AutoMount, true) && cookieBool(control.Banner.Enabled, true),
		"cookieSettingsMounted": control.Enabled && cookieBool(control.AutoMount, true) && cookieBool(control.Settings.Enabled, true),
		"cookieShowCookieList":  cookieBool(control.Settings.ShowCookieList, true),
		"cookieBannerPosition":  control.Banner.Position,
		"cookieConfigJSON":      cookieConfigJSON(),
		"cookieTexts":           texts,
	}
}

type DefaultLayoutCookieData struct{ Noop }

func (DefaultLayoutCookieData) Render(ctx *Context, _ Props) (Data, error) {
	cfg := MustRuntimeConfig()
	items, err := json.Marshal(cfg.QuickCommands.Items)
	if err != nil {
		return nil, err
	}
	data := Data{
		"siteSearchEnabled": cfg.SiteSearch.Enabled, "siteSearchKey": cfg.SiteSearch.Shortcut.Key, "siteSearchCtrlOrMeta": cfg.SiteSearch.Shortcut.CtrlOrMeta, "siteSearchShift": cfg.SiteSearch.Shortcut.Shift,
		"siteSearchMinQueryLength": cfg.SiteSearch.MinQueryLength, "siteSearchLimit": cfg.SiteSearch.Limit, "siteSearchMaxPages": cfg.SiteSearch.MaxPages, "siteSearchCacheTTL": cfg.SiteSearch.CacheTTL,
		"siteSearchServerSearch": cfg.SiteSearch.ServerSearch,
		"quickCommandsEnabled":   cfg.QuickCommands.Enabled, "quickCommandsKey": cfg.QuickCommands.Shortcut.Key, "quickCommandsCtrlOrMeta": cfg.QuickCommands.Shortcut.CtrlOrMeta, "quickCommandsShift": cfg.QuickCommands.Shortcut.Shift,
		"quickCommandItemsJSON": string(items),
	}
	if !cfg.CookieControl.Enabled {
		data["cookieControlEnabled"] = false
		return data, nil
	}
	for key, value := range cookieData(ctx) {
		data[key] = value
	}
	return data, nil
}

type CookiePrivacyPolicyData struct{ Noop }

func (CookiePrivacyPolicyData) Render(ctx *Context, props Props) (Data, error) {
	data := cookieData(ctx)
	title := strings.TrimSpace(props.String("title"))
	if title == "" {
		title, _ = data["cookieTexts"].(map[string]any)["policyTitle"].(string)
	}
	data["cookiePolicyTitle"] = title
	data["cookiePolicyShowSources"] = defaultBool(props, "showSources", true)
	return data, nil
}

type CookieConsentWrapperData struct{ Noop }

func (CookieConsentWrapperData) Render(ctx *Context, props Props) (Data, error) {
	cfg := MustRuntimeConfig()
	if !cfg.CookieControl.Enabled {
		return Data{"cookieControlEnabled": false}, nil
	}
	category := strings.ToLower(strings.TrimSpace(props.String("category")))
	if !cookieCategories[category] {
		category = "functional"
	}
	service := strings.TrimSpace(props.String("serviceName"))
	label := cookieText(ctx, category, strings.ToUpper(category[:1])+category[1:])
	message := fmt.Sprintf(cookieText(ctx, "allowCategory", "Allow %s cookies to display this content."), label)
	if service != "" {
		message = fmt.Sprintf(cookieText(ctx, "allowService", "Allow %s cookies to use %s."), label, service)
	}
	return Data{
		"cookieControlEnabled":  cfg.CookieControl.Enabled,
		"cookieConfigJSON":      cookieConfigJSON(),
		"cookieConsentCategory": category,
		"cookieConsentService":  service,
		"cookieConsentMessage":  message,
		"cookieContentHidden":   cookieText(ctx, "contentHidden", "Content hidden"),
		"cookieSetPrefs":        cookieText(ctx, "setPrefs", "Set preferences"),
	}, nil
}

type ConsentYoutubeData struct{ Noop }

func (ConsentYoutubeData) Render(_ *Context, props Props) (Data, error) {
	id := strings.TrimSpace(props.String("videoId"))
	id = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return -1
	}, id)
	return Data{"consentYoutubeID": id, "consentYoutubeSrc": "https://www.youtube-nocookie.com/embed/" + id, "consentYoutubeHasID": id != ""}, nil
}

type ConsentGoogleMapData struct{ Noop }

func (ConsentGoogleMapData) Render(_ *Context, props Props) (Data, error) {
	query := strings.TrimSpace(props.String("address"))
	if placeID := strings.TrimSpace(props.String("placeId")); placeID != "" {
		query = "place_id:" + placeID
	}
	values := url.Values{"q": {query}, "output": {"embed"}, "hl": {defaultString(strings.TrimSpace(props.String("language")), "en")}, "gl": {defaultString(strings.TrimSpace(props.String("region")), "pl")}}
	title := defaultString(strings.TrimSpace(props.String("title")), "Google Maps")
	return Data{"consentMapHasQuery": query != "", "consentMapSrc": "https://www.google.com/maps?" + values.Encode(), "consentMapTitle": title}, nil
}
