package goserver

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	logic "github.com/myelophone/goserver/web/runtime"
)

type WebApp = App

func NewWebApp() (*WebApp, error) { return NewApp() }

func WebEnabled() (bool, error) {
	config, err := logic.UseRuntimeConfig()
	return config.Runtime.Enabled, err
}

func (s *Server) EnableWebIfEnabled() error {
	enabled, err := WebEnabled()
	if err != nil {
		return fmt.Errorf("read web configuration: %w", err)
	}
	if !enabled {
		return nil
	}
	return s.EnableWeb()
}

func (s *Server) EnableWeb() error {
	s.webMu.Lock()
	defer s.webMu.Unlock()
	if s.web != nil {
		return fmt.Errorf("web application is already enabled")
	}
	app, err := newAppWithLogger(s.Logger)
	if err != nil {
		return fmt.Errorf("initialize web application: %w", err)
	}
	app.errorRenderer = s.RenderError
	app.responder = s
	app.renderer.logger = s.Logger
	app.publicFileHandler = s.PublicFiles
	app.webCache = s.Cache
	app.translate = func(ctx context.Context, key string) string {
		if s.I18n == nil {
			return key
		}
		return s.I18n.L(ctx, key)
	}
	app.translateLang = func(key, lang string) string {
		if s.I18n == nil {
			return key
		}
		return s.I18n.Lang(key, lang)
	}
	app.RegisterRoutes(s)
	if app.config.SiteSearch.Enabled && app.config.SiteSearch.ServerSearch {
		if err := app.rebuildSiteSearchIndex(); err != nil {
			return fmt.Errorf("build server site-search index: %w", err)
		}
	}
	s.web = app
	s.SetNotFoundHandler(app.wrapRequest(http.HandlerFunc(app.pageHandler)).ServeHTTP)
	return nil
}

func (s *Server) Web() *WebApp {
	s.webMu.RLock()
	defer s.webMu.RUnlock()
	return s.web
}

func (s *Server) RebuildSiteSearchIndex() error {
	s.webMu.RLock()
	app := s.web
	s.webMu.RUnlock()
	if app == nil || !app.config.SiteSearch.Enabled || !app.config.SiteSearch.ServerSearch {
		return nil
	}
	return app.rebuildSiteSearchIndex()
}

func CanonicalURL(value string) string {
	if value == "" {
		return "/"
	}
	clean := path.Clean("/" + strings.TrimSpace(value))
	if clean != "/" {
		clean = strings.TrimRight(clean, "/")
	}
	return clean
}
