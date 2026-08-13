package goserver

import (
	"net/http"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (s *Server) TemplatesMiddleware(tm *TemplateManager) {
	caser := cases.Title(language.Und)

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for name := range tm.templates {
		page := name
		route := "/" + strings.TrimSuffix(page, ".html")
		if route == "/index" {
			route = "/"
		}

		s.GET(route, func(w http.ResponseWriter, r *http.Request) {
			tm.mu.RLock()
			globalScripts := tm.GlobalScripts
			globalStyles := tm.GlobalStyles
			globalHead := tm.GlobalHead
			tm.mu.RUnlock()

			data := PageData{
				Title:         caser.String(strings.TrimSuffix(page, ".html")),
				Description:   "Default description for " + page,
				GlobalStyles:  globalStyles,
				GlobalHead:    globalHead,
				GlobalScripts: globalScripts,
			}

			htmlContent, err := tm.RenderHTML(page, data)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			s.RespondHTML(w, htmlContent)
		})
	}
}
