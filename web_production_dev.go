//go:build !myelophone_prod

package goserver

import (
	"io/fs"
	"net/http"
)

func productionSourceFS() (fs.FS, bool)                { return nil, false }
func productionTailwindCSS() (map[string]string, bool) { return nil, false }
func productionClientEntry() ([]byte, bool)            { return nil, false }
func productionRouteChunks() (map[string][]byte, map[string]map[string]string, bool) {
	return nil, nil, false
}

func (a *App) runtimeHandler(w http.ResponseWriter, r *http.Request) {
	serveCachedJS(w, r, embeddedRuntimeJS, false)
}
