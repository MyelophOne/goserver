//go:build !myelophone_prod

package goserver

import (
	"fmt"
	"strings"

	logic "github.com/myelophone/goserver/web/runtime"
)

func loadApplicationAssets(pages *Pages, components, layouts, teleports *Components, cfg logic.RuntimeConfig) (TailwindBuild, []byte, map[string]bool, error) {
	var err error
	tailwind := TailwindBuild{CSSByPage: map[string]string{}, Sources: map[string][]string{}}
	if prebuilt, ok := productionTailwindCSS(); ok {
		tailwind.CSSByPage = prebuilt
	} else {
		tailwind, err = BuildTailwind(pages, components, layouts, cfg)
		if err != nil {
			return tailwind, nil, nil, err
		}
	}
	scriptUsage := AnalyzeScriptUsage(pages, components)
	sharedSources := sharedLayoutScriptSources(layouts)
	for id, source := range sharedTeleportScriptSources(teleports, components) {
		sharedSources[id] = source
	}
	for id, source := range reusableComponentScriptSources(components, scriptUsage) {
		sharedSources[id] = source
	}
	entrySources := map[string]bool{}
	clientEntry, ok := productionClientEntry()
	if !ok {
		clientEntry, err = BuildClientEntry(sharedSources)
		if err != nil {
			clientEntry = embeddedRuntimeJS
		} else if clientEntrySupportsModuleRegistry(clientEntry) {
			entrySources = sourceIDSet(sharedSources)
		}
	} else if clientEntrySupportsModuleRegistry(clientEntry) {
		entrySources = sourceIDSet(sharedSources)
	}

	return tailwind, clientEntry, entrySources, nil
}

func (a *App) precompileRouteClientChunks() error {
	if _, production := productionClientEntry(); production {
		chunks, routes, ok := productionRouteChunks()
		if !ok {
			return nil
		}
		for id, chunk := range chunks {
			a.chunks[id] = string(chunk)
		}
		for page, sources := range routes {
			if a.sourceChunks[page] == nil {
				a.sourceChunks[page] = map[string]string{}
			}
			for sourceID, chunkID := range sources {
				a.sourceChunks[page][sourceID] = "/_gosh/chunk/" + chunkID + ".js"
			}
		}
		return nil
	}
	if a.pages == nil {
		return nil
	}
	for _, page := range append(append([]*Page{}, a.pages.items...), a.pages.NotFound, a.pages.ErrorPage) {
		if page == nil || page.View == nil {
			continue
		}
		sources := routeClientSources(page, a.components, a.layouts, a.config.Render.DefaultLayout, a.entrySources, a.scriptUsage, a.config.CookieControl.Enabled)
		if len(sources) == 0 {
			continue
		}
		chunk, err := BuildRouteClientChunk(sources)
		if err != nil {
			return err
		}
		id := hashText(string(chunk))
		a.chunks[id] = string(chunk)
		url := "/_gosh/chunk/" + id + ".js"
		if a.sourceChunks[page.RelativePath] == nil {
			a.sourceChunks[page.RelativePath] = map[string]string{}
		}
		for sourceID := range sources {
			a.sourceChunks[page.RelativePath][sourceID] = url
		}
	}
	return nil
}

func (a *App) precompileStaticStyles() error {
	if a.precompiledStyles == nil {
		a.precompiledStyles = map[string][]string{}
	}
	if bundle, production := logic.UseProductionBundle(); production {
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
	compile := func(css string) (string, error) {
		if strings.TrimSpace(css) == "" {
			return "", nil
		}
		rawID := hashText(css)
		processed, err := processFinalCSS(css)
		if err != nil {
			return "", err
		}
		processed = minifyCSS(processed)
		id := hashText(processed)
		a.finalCSS[rawID] = processed
		a.styles[id] = processed
		return "/_gosh/style/" + id + ".css", nil
	}
	if !a.config.Render.SplitCSS {
		url, err := compile(a.unifiedCSS)
		if err != nil {
			return fmt.Errorf("precompile application CSS: %w", err)
		}
		for _, page := range pageList(a.pages) {
			if url != "" {
				a.precompiledStyles[page.RelativePath] = []string{url}
			}
		}
		return nil
	}

	common := []string{}
	for _, chunk := range buildCSSChunks([]string{a.commonCSS}, true, a.config.Render.CSSMinChunkSize, a.config.Render.CSSMaxChunkSize) {
		url, err := compile(chunk)
		if err != nil {
			return fmt.Errorf("precompile common CSS: %w", err)
		}
		if url != "" {
			common = append(common, url)
		}
	}
	for _, page := range pageList(a.pages) {
		parts := a.staticCSSForPage(page)
		urls := append([]string(nil), common...)
		for _, chunk := range buildCSSChunks(parts, true, a.config.Render.CSSMinChunkSize, a.config.Render.CSSMaxChunkSize) {
			url, err := compile(chunk)
			if err != nil {
				return fmt.Errorf("precompile CSS for %s: %w", page.RelativePath, err)
			}
			if url != "" {
				urls = append(urls, url)
			}
		}
		a.precompiledStyles[page.RelativePath] = urls
	}
	return nil
}
