package goserver

import (
	"io/fs"
	"net/http"

	logic "github.com/myelophone/goserver/web/runtime"
)

func productionSourceFS() (fs.FS, bool) {
	bundle, ok := logic.UseProductionBundle()
	return bundle.Sources, ok
}
func productionTailwindCSS() (map[string]string, bool) {
	bundle, ok := logic.UseProductionBundle()
	return bundle.TailwindCSS, ok
}
func productionClientEntry() ([]byte, bool) {
	bundle, ok := logic.UseProductionBundle()
	return bundle.ClientEntry, ok
}
func productionRouteChunks() (map[string][]byte, map[string]map[string]string, bool) {
	bundle, ok := logic.UseProductionBundle()
	return bundle.RouteChunks, bundle.RouteMaps, ok
}

func (a *App) runtimeHandler(w http.ResponseWriter, r *http.Request) {
	serveCachedJS(w, r, embeddedRuntimeJS, false)
}
