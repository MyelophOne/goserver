package runtime

import (
	"io/fs"
	"sync"
)

type ProductionBundle struct {
	Sources     fs.FS
	ClientEntry []byte
	TailwindCSS map[string]string
	FinalCSS    map[string]string
	StyleAssets map[string]string
	PageStyles  map[string][]string
	RouteChunks map[string][]byte
	RouteMaps   map[string]map[string]string
}

var productionState = struct {
	sync.RWMutex
	bundle *ProductionBundle
	config map[string][]byte
}{}

func RegisterProductionBundle(bundle ProductionBundle) {
	productionState.Lock()
	defer productionState.Unlock()
	productionState.bundle = &bundle
}

func UseProductionBundle() (ProductionBundle, bool) {
	productionState.RLock()
	defer productionState.RUnlock()
	if productionState.bundle == nil {
		return ProductionBundle{}, false
	}
	return *productionState.bundle, true
}

func RegisterProductionConfig(files map[string][]byte) {
	productionState.Lock()
	defer productionState.Unlock()
	productionState.config = make(map[string][]byte, len(files))
	for path, data := range files {
		productionState.config[path] = append([]byte(nil), data...)
	}
}
