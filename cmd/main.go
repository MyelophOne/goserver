//go:build !webcli

package main

import (
	"github.com/myelophone/goserver"
	logic "github.com/myelophone/goserver/web/runtime"
)

func main() {
	httpPort := goserver.GetEnv("HTTP_PORT", "8080")
	s := goserver.NewServer(httpPort)

	s.Defaults()
	defaultLanguage := s.Config.I18nDefaultLanguage
	languages := s.Config.I18nLanguages
	webConfig, err := logic.UseRuntimeConfig()
	if err != nil {
		s.Logger.Fatal(err)
	}
	if webConfig.Runtime.Enabled {
		if webConfig.DefaultLocale != "" {
			defaultLanguage = webConfig.DefaultLocale
		}
		if len(webConfig.Locales) > 0 {
			languages = webConfig.Locales
		}
	}
	if _, err := s.NewI18n(defaultLanguage, languages); err != nil {
		s.Logger.Fatal(err)
	}

	if err := s.EnableWebIfEnabled(); err != nil {
		s.Logger.Fatal(err)
	}

	s.Run()
}
