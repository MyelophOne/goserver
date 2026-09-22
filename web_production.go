//go:build !myelophone_prod

package goserver

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	logic "github.com/myelophone/goserver/web/runtime"
)

func reachableProductionFiles(pages *Pages, components, layouts *Components, defaultLayout string, teleportRoots []*Component) []string {
	set := map[string]bool{}
	add := func(p string) {
		if p != "" {
			if absolute, err := filepath.Abs(p); err == nil {
				if base, baseErr := filepath.Abs("."); baseErr == nil {
					if relative, relErr := filepath.Rel(base, absolute); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
						p = relative
					}
				}
			}
			set[sourcePath(p)] = true
		}
	}
	for _, pg := range pageList(pages) {
		if pg == nil || pg.View == nil {
			continue
		}
		add(filepath.Join(systemPagesDir, pg.RelativePath))
		for _, view := range reachableViewsForPage(pg, components, layouts, defaultLayout) {
			if view == pg.View {
				continue
			}
			if layouts != nil {
				if l, ok := layouts.Get(view.Name); ok && l == view {
					add(filepath.Join(systemLayoutsDir, view.RelativePath))
					continue
				}
			}
			add(filepath.Join(systemComponentsDir, view.RelativePath))
		}
	}
	for _, teleport := range teleportRoots {
		if teleport == nil {
			continue
		}
		for _, view := range reachableViewsForPage(&Page{View: teleport, Layout: "none"}, components, nil, "") {
			add(view.Path)
		}
	}
	for _, sub := range []string{"head", "styles", "scripts"} {
		dir := filepath.Join(systemGlobalDir, sub)
		entries, err := sourceReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".gosh") {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}
	_ = sourceWalkDir(systemContentDir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			add(path)
		}
		return nil
	})
	add(filepath.Join(systemTemplatesDir, "loader.gosh"))
	add(filepath.Join(systemTemplatesDir, "spa-loading-template.html"))
	for _, optional := range []string{systemTailwindGlobal, filepath.Join(systemFrameworkCSSDir, "default.css"), filepath.Join(systemWebCSSDir, "default.css")} {
		if info, err := fs.Stat(webSourceFS(), sourcePath(optional)); err == nil && !info.IsDir() {
			_, imports, importErr := loadCSSWithImports(optional)
			if importErr != nil {
				continue
			}
			for _, imported := range imports {
				add(imported)
			}
		}
	}
	for _, root := range []string{systemTeleportDir, systemFrameworkTeleportDir} {
		_ = sourceWalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".gosh") {
				add(path)
			}
			return nil
		})
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func tenantProductionFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(tenantRootDir, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(tenantRootDir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) < 3 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		include := (parts[1] == "pages" || parts[1] == "components" || parts[1] == "layouts" || parts[1] == "teleport" || parts[1] == "global") && ext == ".gosh"
		include = include || parts[1] == "content" && ext == ".md"
		include = include || (parts[1] == "css" && ext == ".css")
		include = include || ((parts[1] == "plugins" || parts[1] == "stores") && (ext == ".js" || ext == ".mjs"))
		if include {
			files = append(files, path)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Strings(files)
	return files, err
}

func tenantPageSets() (map[string]*Pages, error) {
	entries, err := os.ReadDir(tenantRootDir)
	if os.IsNotExist(err) {
		return map[string]*Pages{}, nil
	}
	if err != nil {
		return nil, err
	}
	sets := make(map[string]*Pages)
	for _, entry := range entries {
		if !entry.IsDir() || !safeTenantID(entry.Name()) {
			continue
		}
		root := filepath.Join(tenantRootDir, entry.Name(), "pages")
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		pages, err := LoadPages(root)
		if err != nil {
			return nil, fmt.Errorf("load tenant %q pages: %w", entry.Name(), err)
		}
		sets[entry.Name()] = pages
	}
	return sets, nil
}

func tenantReachableProductionFiles(sets map[string]*Pages, components, layouts *Components, defaultLayout string) []string {
	set := map[string]bool{}
	for id, pages := range sets {
		for _, page := range pageList(pages) {
			if page == nil || page.View == nil {
				continue
			}
			set[sourcePath(filepath.Join(tenantRootDir, id, "pages", page.RelativePath))] = true
			for _, view := range reachableViewsForPage(page, components, layouts, defaultLayout) {
				if view == page.View {
					continue
				}
				if layouts != nil {
					if layout, ok := layouts.Get(view.Name); ok && layout == view {
						set[sourcePath(filepath.Join(systemLayoutsDir, view.RelativePath))] = true
						continue
					}
				}
				set[sourcePath(filepath.Join(systemComponentsDir, view.RelativePath))] = true
			}
		}
	}
	files := make([]string, 0, len(set))
	for file := range set {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func mergeDependencyGraph(dst, src *DependencyGraph) {
	for name := range src.Components {
		dst.Components[name] = true
	}
	for name, ref := range src.ServerExports {
		dst.ServerExports[name] = ref
	}
}

func goQuote(s string) string { return strconv.Quote(s) }

func writeProductionEmbed(files []string, bundle logic.ProductionBundle) (string, error) {
	tailwind, finalCSS := bundle.TailwindCSS, bundle.FinalCSS
	clientEntry, routeChunks, routeMaps := bundle.ClientEntry, bundle.RouteChunks, bundle.RouteMaps
	generatedDir := filepath.Join(generatedWebDir, "production")
	if err := os.RemoveAll(generatedDir); err != nil {
		return "", err
	}
	sourcesDir := filepath.Join(generatedDir, "sources")
	if err := os.MkdirAll(sourcesDir, 0o755); err != nil {
		return "", err
	}

	for _, original := range files {
		virtual := sourcePath(original)
		data, err := sourceReadFile(original)
		if err != nil {
			return "", fmt.Errorf("read reachable %s: %w", original, err)
		}
		if !fs.ValidPath(virtual) {
			return "", fmt.Errorf("invalid production source path %q", virtual)
		}
		target := filepath.Join(sourcesDir, filepath.FromSlash(virtual))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	if data, err := fs.ReadFile(publicAssetsFS(systemPublicDir), "robots.txt"); err == nil {
		target := filepath.Join(sourcesDir, "assets", "robots.txt")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	clientEntryFile := filepath.Join(sourcesDir, "client-entry-"+hashText(string(clientEntry))+".js")
	if err := os.WriteFile(clientEntryFile, clientEntry, 0o644); err != nil {
		return "", err
	}
	routeChunkFiles := map[string]string{}
	for id, data := range routeChunks {
		name := filepath.Join(sourcesDir, "route-"+id+".js")
		if err := os.WriteFile(name, data, 0o644); err != nil {
			return "", err
		}
		routeChunkFiles[id] = productionEmbedPath(name)
	}

	pageCSSFiles := map[string]string{}
	for pageName, css := range tailwind {
		h := hashText("tailwind:" + pageName + ":" + css)
		name := filepath.Join(sourcesDir, h+".css")
		if err := os.WriteFile(name, []byte(css), 0o644); err != nil {
			return "", err
		}
		pageCSSFiles[pageName] = productionEmbedPath(name)
	}

	var b strings.Builder
	b.WriteString("//go:build myelophone_prod\n\npackage generated\n\nimport (\n\t\"embed\"\n\t\"io/fs\"\n\truntime \"github.com/myelophone/goserver/web/runtime\"\n)\n\n")
	b.WriteString("//go:embed all:production/sources\nvar myelophoneProductionFS embed.FS\n\n")
	b.WriteString("func productionSourceFS() (fs.FS, bool) {\n\tsources, err := fs.Sub(myelophoneProductionFS, \"production/sources\")\n\treturn sources, err == nil\n}\n\n")
	b.WriteString("func productionClientEntry() ([]byte, bool) {\n")
	b.WriteString("\tb, _ := myelophoneProductionFS.ReadFile(")
	b.WriteString(goQuote(productionEmbedPath(clientEntryFile)))
	b.WriteString(")\n\treturn b, true\n}\n\n")
	b.WriteString("func productionTailwindCSS() (map[string]string, bool) {\n\tout := map[string]string{}\n")
	cssKeys := make([]string, 0, len(pageCSSFiles))
	for k := range pageCSSFiles {
		cssKeys = append(cssKeys, k)
	}
	sort.Strings(cssKeys)
	for i, k := range cssKeys {
		name := fmt.Sprintf("css%d", i)
		b.WriteString("\t" + name + ", _ := myelophoneProductionFS.ReadFile(")
		b.WriteString(goQuote(pageCSSFiles[k]))
		b.WriteString(")\n\tout[")
		b.WriteString(goQuote(k))
		b.WriteString("] = string(" + name + ")\n")
	}
	b.WriteString("\treturn out, true\n}\n")
	b.WriteString("\nfunc productionRouteChunks() (map[string][]byte, map[string]map[string]string, bool) {\n\tchunks := map[string][]byte{}\n")
	routeIDs := make([]string, 0, len(routeChunkFiles))
	for id := range routeChunkFiles {
		routeIDs = append(routeIDs, id)
	}
	sort.Strings(routeIDs)
	for _, id := range routeIDs {
		name := "route" + hashText(id)
		b.WriteString("\t" + name + ", _ := myelophoneProductionFS.ReadFile(")
		b.WriteString(goQuote(routeChunkFiles[id]))
		b.WriteString(")\n\tchunks[")
		b.WriteString(goQuote(id))
		b.WriteString("] = ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	b.WriteString("\troutes := map[string]map[string]string{}\n")
	pageKeys := make([]string, 0, len(routeMaps))
	for page := range routeMaps {
		pageKeys = append(pageKeys, page)
	}
	sort.Strings(pageKeys)
	for _, page := range pageKeys {
		b.WriteString("\troutes[")
		b.WriteString(goQuote(page))
		b.WriteString("] = map[string]string{")
		sourceIDs := make([]string, 0, len(routeMaps[page]))
		for sourceID := range routeMaps[page] {
			sourceIDs = append(sourceIDs, sourceID)
		}
		sort.Strings(sourceIDs)
		for i, sourceID := range sourceIDs {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(goQuote(sourceID))
			b.WriteByte(':')
			b.WriteString(goQuote(routeMaps[page][sourceID]))
		}
		b.WriteString("}\n")
	}
	b.WriteString("\treturn chunks, routes, true\n}\n")
	b.WriteString("\nfunc init() {\n\tsources, _ := productionSourceFS()\n\tclient, _ := productionClientEntry()\n\tcss, _ := productionTailwindCSS()\n\tchunks, routes, _ := productionRouteChunks()\n\tfinalCSS := map[string]string{\n")
	finalIDs := make([]string, 0, len(finalCSS))
	for id := range finalCSS {
		finalIDs = append(finalIDs, id)
	}
	sort.Strings(finalIDs)
	for _, id := range finalIDs {
		fmt.Fprintf(&b, "\t\t%q: %q,\n", id, finalCSS[id])
	}
	b.WriteString("\t}\n")
	fmt.Fprintf(&b, "\truntime.RegisterProductionBundle(runtime.ProductionBundle{Sources: sources, ClientEntry: client, TailwindCSS: css, FinalCSS: finalCSS, StyleAssets: %#v, PageStyles: %#v, RouteChunks: chunks, RouteMaps: routes})\n}\n", bundle.StyleAssets, bundle.PageStyles)
	file := filepath.Join(generatedWebDir, "production_embed_gen.go")
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return file, nil
}

func productionEmbedPath(path string) string {
	rel, err := filepath.Rel(generatedWebDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func writeWebEntry() (string, error) {
	modulePath, err := webProjectModulePath()
	if err != nil {
		return "", err
	}
	file := filepath.Join("cmd", "web_import_gen.go")
	data := fmt.Sprintf("//go:build !webcli || webbuild\n\npackage main\n\nimport _ %q\n", modulePath+"/"+generatedWebDir)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	if current, err := os.ReadFile(file); err == nil && string(current) == data {
		return file, nil
	}
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		return "", err
	}
	return file, nil
}

func writeProductionRuntimeConfig(cfg logic.RuntimeConfig) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("//go:build myelophone_prod\n\npackage generated\n\nimport runtime \"github.com/myelophone/goserver/web/runtime\"\n\nfunc init() {\n\truntime.RegisterProductionConfig(map[string][]byte{\n")
	b.WriteString("\t\t\"websettings.json\": []byte(" + goQuote(string(data)) + "),\n")
	b.WriteString("\t})\n}\n")
	file := filepath.Join(generatedWebDir, "production_config_gen.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return file, nil
}

func copyTree(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return os.MkdirAll(dst, 0o755)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func copyContentResources(src, dst string, cfg logic.RuntimeConfig) (int, error) {
	if err := os.RemoveAll(dst); err != nil {
		return 0, err
	}
	count := 0
	err := sourceWalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != src {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := sourceReadFile(path)
		if err != nil {
			return err
		}
		if ok, err := optimizePublicImageData(path, target, data, cfg); err != nil {
			return err
		} else if !ok {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
				return err
			}
		}
		count++
		return nil
	})
	if os.IsNotExist(err) {
		err = nil
	}
	return count, err
}

func copyTenantContentResources(dst string, cfg logic.RuntimeConfig) (int, error) {
	entries, err := os.ReadDir(tenantRootDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() || !safeTenantID(entry.Name()) {
			continue
		}
		copied, err := copyContentResources(
			filepath.Join(tenantRootDir, entry.Name(), "content"),
			filepath.Join(dst, "tenants", entry.Name(), "content"),
			cfg,
		)
		if err != nil {
			return 0, fmt.Errorf("copy tenant %q content resources: %w", entry.Name(), err)
		}
		count += copied
	}
	return count, nil
}

func BuildProduction() error { return buildProduction("", false) }

func writeProductionAssetReport(path string) error { return buildProduction(path, true) }

func buildProductionRouteChunks(pages *Pages, components, layouts *Components, defaultLayout string, entrySources map[string]bool, usage map[string]int, cookieControlEnabled bool) (map[string][]byte, map[string]map[string]string, error) {
	chunks := map[string][]byte{}
	routes := map[string]map[string]string{}
	if pages == nil {
		return chunks, routes, nil
	}
	for _, page := range append(append([]*Page{}, pages.items...), pages.NotFound, pages.ErrorPage) {
		if page == nil || page.View == nil {
			continue
		}
		sources := routeClientSources(page, components, layouts, defaultLayout, entrySources, usage, cookieControlEnabled)
		if len(sources) == 0 {
			continue
		}
		data, err := BuildRouteClientChunk(sources)
		if err != nil {
			return nil, nil, err
		}
		chunkID := hashText(string(data))
		chunks[chunkID] = data
		if routes[page.RelativePath] == nil {
			routes[page.RelativePath] = map[string]string{}
		}
		for sourceID := range sources {
			routes[page.RelativePath][sourceID] = chunkID
		}
	}
	return chunks, routes, nil
}

func buildProduction(assetReportPath string, assetsOnly bool) error {
	if _, prod := sourceFS(); prod {
		return fmt.Errorf("production binary cannot build itself")
	}
	playgroundEnabled.Store(false)
	if !assetsOnly {
		fmt.Println("MyelophOne production build")
	}
	cfg, err := logic.UseRuntimeConfig()
	if err != nil {
		return err
	}
	if err := logic.SetupModules(cfg); err != nil {
		return err
	}
	if err := logic.CallHook(logic.HookBuildBefore, &logic.HookPayload{Values: map[string]any{"command": "build"}}); err != nil {
		return err
	}
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		return err
	}
	teleports, err := LoadComponents(systemTeleportDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	teleportRoots := make([]*Component, 0)
	if teleports != nil {
		for _, name := range teleports.Names() {
			component, _ := teleports.Get(name)
			teleportRoots = append(teleportRoots, component)
			key := normalizeComponentLookup(name)
			if _, exists := components.items[key]; exists {
				return fmt.Errorf("teleport component %q conflicts with a regular component", name)
			}
			components.items[key] = component
		}
		components.names = componentNames(components.items)
	}
	systemTeleports, err := LoadComponents(systemFrameworkTeleportDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if systemTeleports != nil {
		for _, name := range systemTeleports.Names() {
			component, _ := systemTeleports.Get(name)
			teleportRoots = append(teleportRoots, component)
			key := normalizeComponentLookup(name)
			if _, exists := components.items[key]; exists {
				return fmt.Errorf("system teleport component %q conflicts with an existing component", name)
			}
			components.items[key] = component
		}
		components.names = componentNames(components.items)
	}
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		return err
	}
	layouts, err := LoadComponents(systemLayoutsDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if layouts == nil {
		layouts = &Components{items: map[string]*Component{}}
	}
	ensureDefaultLayout(layouts, cfg.Render.DefaultLayout)
	graph := BuildDependencyGraphWithLayouts(pages, components, layouts, cfg.Render.DefaultLayout)
	tenantPages, err := tenantPageSets()
	if err != nil {
		return fmt.Errorf("collect tenant pages: %w", err)
	}
	for _, tenantPages := range tenantPages {
		mergeDependencyGraph(graph, BuildDependencyGraphWithLayouts(tenantPages, components, layouts, cfg.Render.DefaultLayout))
	}
	if err := logic.CallHook(logic.HookBuildGraph, &logic.HookPayload{Data: graph}); err != nil {
		return err
	}
	unusedAssets := map[string]bool{}
	if !assetsOnly {
		report := BuildUnusedReport(pages, components, layouts, cfg)
		if err := WriteUnusedReport(report); err != nil {
			return err
		}
		for _, file := range report.Files {
			if file.Kind == "asset" {
				unusedAssets[sourcePath(file.Path)] = true
			}
		}
		PrintUnusedReport(report)
	}
	if err := logic.CallHook(logic.HookBuildClientBefore, &logic.HookPayload{}); err != nil {
		return err
	}
	usage := AnalyzeScriptUsage(pages, components)
	sharedSources := sharedLayoutScriptSources(layouts)
	for id, source := range sharedTeleportScriptSources(teleports, components) {
		sharedSources[id] = source
	}
	for id, source := range reusableComponentScriptSources(components, usage) {
		sharedSources[id] = source
	}
	clientEntry, err := BuildClientEntry(sharedSources)
	if err != nil {
		return err
	}
	routeChunks, routeMaps, err := buildProductionRouteChunks(pages, components, layouts, cfg.Render.DefaultLayout, sourceIDSet(sharedSources), usage, cfg.CookieControl.Enabled)
	if err != nil {
		return err
	}
	if err := logic.CallHook(logic.HookBuildClientAfter, &logic.HookPayload{Data: len(clientEntry)}); err != nil {
		return err
	}
	if err := logic.CallHook(logic.HookBuildTailwindBefore, &logic.HookPayload{}); err != nil {
		return err
	}
	tw, err := BuildTailwind(pages, components, layouts, cfg)
	if err != nil {
		return err
	}
	if err := logic.CallHook(logic.HookBuildTailwindAfter, &logic.HookPayload{Data: tw.CSSByPage}); err != nil {
		return err
	}
	assets := []generatedAsset{
		{Path: "/_gosh/entry/" + hashText(string(clientEntry)) + ".js", Data: withGeneratedJSBanner(clientEntry)},
		{Path: "/_gosh/websocket/" + hashText(string(embeddedWebSocketJS)) + ".js", Data: withGeneratedJSBanner(embeddedWebSocketJS)},
	}
	if cfg.Runtime.WebVitals {
		assets = append(assets, generatedAsset{Path: "/_gosh/vitals/" + hashText(string(embeddedVitalsJS)) + ".js", Data: withGeneratedJSBanner(embeddedVitalsJS)})
	}
	for page, css := range tw.CSSByPage {
		if strings.TrimSpace(css) == "" {
			continue
		}
		body := []byte(withGeneratedAssetBanner(css))
		assets = append(assets, generatedAsset{Path: "/_gosh/style/tailwind-" + hashText(page+":"+css) + ".css", Data: body})
	}
	if err := logic.CallHook(logic.HookBuildAssetsBefore, &logic.HookPayload{Data: assets}); err != nil {
		return err
	}
	assetReport := generatedAssetReport(assets)
	if err := logic.CallHook(logic.HookBuildAssetsAfter, &logic.HookPayload{Data: assetReport}); err != nil {
		return err
	}
	if assetsOnly {
		return writeGeneratedAssetReport(assetReportPath, assetReport)
	}
	files := reachableProductionFiles(pages, components, layouts, cfg.Render.DefaultLayout, teleportRoots)
	tenantFiles, err := tenantProductionFiles()
	if err != nil {
		return fmt.Errorf("collect tenant web sources: %w", err)
	}
	files = append(files, tenantFiles...)
	files = append(files, tenantReachableProductionFiles(tenantPages, components, layouts, cfg.Render.DefaultLayout)...)
	uniqueFiles := make(map[string]bool, len(files))
	for _, file := range files {
		if file != "" {
			uniqueFiles[file] = true
		}
	}
	files = files[:0]
	for file := range uniqueFiles {
		files = append(files, file)
	}
	sort.Strings(files)
	commonCSS := systemBaseCSS + tw.CSSByPage["@shared"]
	styleTeleports := &Components{items: map[string]*Component{}}
	for _, component := range teleportRoots {
		styleTeleports.items[normalizeComponentLookup(component.Name)] = component
	}
	styleTeleports.names = componentNames(styleTeleports.items)
	styleCompiler := &App{
		components: components, pages: pages, layouts: layouts, config: cfg,
		teleports:   styleTeleports,
		tailwindCSS: tw.CSSByPage, commonCSS: commonCSS,
		unifiedCSS: buildUnifiedCSS(commonCSS, pages, components, layouts),
		styles:     map[string]string{}, finalCSS: map[string]string{},
	}
	if err := styleCompiler.precompileStaticStyles(); err != nil {
		return err
	}
	embedFile, err := writeProductionEmbed(files, logic.ProductionBundle{
		TailwindCSS: tw.CSSByPage, FinalCSS: styleCompiler.finalCSS,
		StyleAssets: styleCompiler.styles, PageStyles: styleCompiler.precompiledStyles,
		ClientEntry: clientEntry, RouteChunks: routeChunks, RouteMaps: routeMaps,
	})
	if err != nil {
		return err
	}
	defer os.Remove(embedFile)
	configEmbedFile, err := writeProductionRuntimeConfig(cfg)
	if err != nil {
		return err
	}
	defer os.Remove(configEmbedFile)
	defer os.RemoveAll(filepath.Join(generatedWebDir, "production"))
	if err := GenerateWebLogicBindings(); err != nil {
		return err
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join("dist", "myelophone-unused.json"))
	for _, name := range []string{"websettings.json", "websettings.Development.json", "websettings.Production.json"} {
		_ = os.Remove(filepath.Join("dist", name))
	}
	out := filepath.Join("dist", "goserver")
	if os.PathSeparator == '\\' {
		out += ".exe"
	}
	linkerFlags := []string{"-s", "-w", "-X", "github.com/myelophone/goserver.AppEnv=prod"}
	if version := strings.TrimSpace(os.Getenv("GIT_COMMIT_HASH")); version != "" {
		linkerFlags = append(linkerFlags, "-X", "github.com/myelophone/goserver.AppVersion="+version)
	}
	if version := strings.TrimSpace(os.Getenv("GOSERVER_VERSION")); version != "" {
		linkerFlags = append(linkerFlags, "-X", "github.com/myelophone/goserver/web/system/logic.BuildVersion="+version)
	}
	cmd := exec.Command("go", "build", "-tags", "myelophone_prod", "-trimpath", "-ldflags", strings.Join(linkerFlags, " "), "-o", out, "./cmd")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	if err := logic.CallHook(logic.HookBuildPublicBefore, &logic.HookPayload{}); err != nil {
		return err
	}
	searchIndexPath := filepath.Join("tmp", "site-search-index.ndjson")
	if err := buildSiteSearchIndex(searchIndexPath); err != nil {
		return err
	}
	unusedAssets["assets/robots.txt"] = true
	if err := copyPublicOptimized(systemPublicDir, filepath.Join("dist", "assets"), cfg, unusedAssets); err != nil {
		return fmt.Errorf("copy/optimize assets: %w", err)
	}
	for _, name := range []string{"site-search-worker.js", "site-search-server-worker.js", "site-search-index.ndjson"} {
		_ = os.Remove(filepath.Join("dist", "assets", name))
	}
	selectedWorker := "site-search-worker.js"
	if cfg.SiteSearch.ServerSearch {
		selectedWorker = "site-search-server-worker.js"
	}
	if data, err := os.ReadFile(filepath.Join("tmp", "site-search", selectedWorker)); err == nil {
		if err := os.WriteFile(filepath.Join("dist", "assets", selectedWorker), data, 0o644); err != nil {
			return fmt.Errorf("write site search worker: %w", err)
		}
	}
	productionSearchIndex := filepath.Join("dist", "assets", "site-search-index.ndjson")
	if cfg.SiteSearch.Enabled && !cfg.SiteSearch.ServerSearch {
		data, err := os.ReadFile(searchIndexPath)
		if err != nil {
			return fmt.Errorf("read site search index: %w", err)
		}
		if err := os.WriteFile(productionSearchIndex, data, 0o644); err != nil {
			return fmt.Errorf("write site search index: %w", err)
		}
	} else {
		_ = os.Remove(productionSearchIndex)
	}
	_, err = copyContentResources(systemContentDir, filepath.Join("dist", "assets", "content"), cfg)
	if err != nil {
		return fmt.Errorf("copy/optimize content resources: %w", err)
	}
	_, err = copyTenantContentResources(filepath.Join("dist", "assets"), cfg)
	if err != nil {
		return err
	}
	if err := logic.CallHook(logic.HookBuildPublicAfter, &logic.HookPayload{Data: filepath.Join("dist", "assets")}); err != nil {
		return err
	}
	fmt.Println("Generated shared assets:")
	for _, asset := range assetReport {
		ext := strings.ToLower(filepath.Ext(asset.Path))
		if ext == ".js" || ext == ".css" {
			fmt.Printf("  %s (%s, gzip %s)\n", asset.Path, formatAssetSize(asset.RawSize), formatAssetSize(asset.GzipSize))
		}
	}
	fmt.Println("Assets copied/optimized to dist/assets")
	if err := logic.CallHook(logic.HookBuildDone, &logic.HookPayload{Data: out}); err != nil {
		return err
	}
	return nil
}

func RunWebCLI(args []string) error {
	if len(args) > 0 && args[0] == "setup" {
		return SetupWebTools()
	}
	if len(args) > 0 && args[0] == "generate" && os.Getenv("MYELOPHONE_WEB_ASSET_REPORT") != "" {
		if err := GenerateWebLogicBindings(); err != nil {
			return err
		}
		return writeProductionAssetReport(os.Getenv("MYELOPHONE_WEB_ASSET_REPORT"))
	}
	if IsProd() && (len(args) == 0 || (args[0] != "build" && args[0] != "generate")) {
		return fmt.Errorf("web CLI is disabled in production")
	}
	if len(args) > 0 && args[0] == "generate" {
		return GenerateWebLogicBindings()
	}
	if len(args) > 0 && args[0] == "clean" {
		for _, path := range []string{"dist", ".myelophone-build", generatedWebDir, "cmd/web_import_gen.go", unusedReportPath, "tmp/myelophone-unused.json", "myelophone-unused.json"} {
			_ = os.RemoveAll(path)
		}
		fmt.Println("MyelophOne generated output cleaned")
		return nil
	}
	if len(args) > 0 && args[0] == "audit" {
		report := BuildUnusedReport(nil, nil, nil, logic.RuntimeConfig{})
		if err := WriteUnusedReport(report); err != nil {
			return err
		}
		PrintUnusedReport(report)
		return nil
	}
	if len(args) > 0 && args[0] == "prune-unused" {
		yes := len(args) > 1 && args[1] == "--yes"
		if err := PruneUnused(yes); err != nil {
			return err
		}
		return nil
	}
	if len(args) > 0 && args[0] == "build" {
		if err := BuildProduction(); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("usage: web <generate|build|audit|clean|prune-unused --yes>")
}
