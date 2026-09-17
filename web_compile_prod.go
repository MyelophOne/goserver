//go:build myelophone_prod

package goserver

import logic "github.com/myelophone/goserver/web/runtime"

func loadApplicationAssets(pages *Pages, components, layouts, teleports *Components, cfg logic.RuntimeConfig) (TailwindBuild, []byte, map[string]bool, error) {
	bundle, _ := logic.UseProductionBundle()
	entrySources := map[string]bool{}
	if clientEntrySupportsModuleRegistry(bundle.ClientEntry) {
		sources := sharedLayoutScriptSources(layouts)
		for id, source := range sharedTeleportScriptSources(teleports, components) {
			sources[id] = source
		}
		for id, source := range reusableComponentScriptSources(components, AnalyzeScriptUsage(pages, components)) {
			sources[id] = source
		}
		entrySources = sourceIDSet(sources)
	}
	return TailwindBuild{CSSByPage: bundle.TailwindCSS}, bundle.ClientEntry, entrySources, nil
}

func (a *App) precompileRouteClientChunks() error {
	bundle, _ := logic.UseProductionBundle()
	for id, chunk := range bundle.RouteChunks {
		a.chunks[id] = string(chunk)
	}
	for page, sources := range bundle.RouteMaps {
		a.sourceChunks[page] = map[string]string{}
		for sourceID, chunkID := range sources {
			a.sourceChunks[page][sourceID] = "/_gosh/chunk/" + chunkID + ".js"
		}
	}
	return nil
}

func (a *App) precompileStaticStyles() error {
	bundle, _ := logic.UseProductionBundle()
	for id, css := range bundle.StyleAssets {
		a.styles[id] = css
	}
	for id, css := range bundle.FinalCSS {
		a.finalCSS[id] = css
	}
	for page, urls := range bundle.PageStyles {
		a.precompiledStyles[page] = append([]string(nil), urls...)
	}
	return nil
}
