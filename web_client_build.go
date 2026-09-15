package goserver

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	logic "github.com/myelophone/goserver/web/runtime"
)

func clientToolBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MYELOPHONE_ESBUILD_BIN")); override != "" {
		if path, err := filepath.Abs(override); err == nil {
			if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
				return path, nil
			}
		}
		if path, err := exec.LookPath(override); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("MYELOPHONE_ESBUILD_BIN=%q does not point to an executable", override)
	}
	candidates := []string{filepath.Join(systemClientDir, "node_modules", ".bin", "esbuild")}
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(systemClientDir, "node_modules", "@esbuild", "win32-x64", "esbuild.exe"),
			filepath.Join(systemClientDir, "node_modules", ".bin", "esbuild.cmd"),
			filepath.Join(systemClientDir, "node_modules", ".bin", "esbuild"),
		}
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath("esbuild"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("client build requires dependencies in web/system/client; run `yarn --cwd web/system/client install`")
}

type clientSource struct {
	path string
	name string
}

func buildSiteSearchWorker(bin string) (string, error) {
	config, err := logic.UseRuntimeConfig()
	if err != nil {
		return "", err
	}
	name := "site-search-worker.js"
	if config.SiteSearch.ServerSearch {
		name = "site-search-server-worker.js"
	}
	source := sourceFilePath(filepath.Join(systemClientDir, name))
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	// A worker is a build product, not a public source file. Keeping it under
	// tmp also prevents a build for one search mode leaking into the other.
	outputDir := filepath.Join("tmp", "site-search")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	output := filepath.Join(outputDir, name)
	other := "site-search-server-worker.js"
	if name == other {
		other = "site-search-worker.js"
	}
	_ = os.Remove(filepath.Join(outputDir, other))
	nodeModules, _ := filepath.Abs(filepath.Join(systemClientDir, "node_modules"))
	cmd := exec.Command(bin, source, "--bundle", "--format=iife", "--platform=browser", "--target=es2020", "--minify", "--outfile="+output)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "NODE_PATH="+nodeModules)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("site-search worker build: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	built, err := os.ReadFile(output)
	if err != nil {
		return "", err
	}
	return "/assets/" + name + "?v=" + hashText(string(built)), nil
}

func clientSourceFiles(root string) ([]clientSource, error) {
	var out []clientSource
	err := sourceWalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".js" || ext == ".mjs" {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, clientSource{path: sourceFilePath(path), name: filepath.ToSlash(strings.TrimSuffix(relative, ext))})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, err
}

func storeSourceFiles() ([]clientSource, error) {
	stores, err := clientSourceFiles(systemStoresDir)
	if err != nil {
		return nil, err
	}
	config, err := logic.UseRuntimeConfig()
	if err != nil {
		return nil, err
	}
	return filterCookieStore(stores, config.CookieControl.Enabled), nil
}

func filterCookieStore(stores []clientSource, enabled bool) []clientSource {
	if enabled {
		return stores
	}
	filtered := stores[:0]
	for _, store := range stores {
		if store.name != "cookies" {
			filtered = append(filtered, store)
		}
	}
	return filtered
}

func clientPluginSourceFiles() ([]clientSource, error) {
	return clientSourceFiles(systemClientPluginsDir)
}

func BuildClientEntry(sharedSources map[string]string) ([]byte, error) {
	bin, err := clientToolBinary()
	if err != nil {
		return nil, err
	}
	stores, err := storeSourceFiles()
	if err != nil {
		return nil, err
	}
	plugins, err := clientPluginSourceFiles()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(systemClientDir, 0o755); err != nil {
		return nil, err
	}
	workerURL, err := buildSiteSearchWorker(bin)
	if err != nil {
		return nil, err
	}
	entry := filepath.Join(systemClientDir, ".myelophone-entry.mjs")
	output := filepath.Join(systemClientDir, ".myelophone-entry.js")
	defer os.Remove(entry)
	defer os.Remove(output)
	sharedModulePaths := make([]string, 0, len(sharedSources))
	defer func() {
		for _, path := range sharedModulePaths {
			_ = os.Remove(path)
		}
	}()

	var b strings.Builder
	runtimePath := sourceFilePath(filepath.Join(systemRuntimeDir, "runtime.js"))
	runtimeRel, _ := filepath.Rel(systemClientDir, runtimePath)
	runtimeRel = filepath.ToSlash(runtimeRel)
	if !strings.HasPrefix(runtimeRel, ".") {
		runtimeRel = "./" + runtimeRel
	}
	b.WriteString("import " + strconv.Quote(runtimeRel) + ";\n")
	b.WriteString("import { createStore } from \"zustand/vanilla\";\n")
	sharedIDs := make([]string, 0, len(sharedSources))
	for id := range sharedSources {
		sharedIDs = append(sharedIDs, id)
	}
	sort.Strings(sharedIDs)
	for i, id := range sharedIDs {
		modulePath := filepath.Join(systemClientDir, ".myelophone-entry-"+id+".mjs")
		if err := os.WriteFile(modulePath, []byte(sharedSources[id]), 0o644); err != nil {
			return nil, fmt.Errorf("write shared client module %s: %w", id, err)
		}
		sharedModulePaths = append(sharedModulePaths, modulePath)
		b.WriteString(fmt.Sprintf("import * as M%d from %s;\n", i, strconv.Quote("./"+filepath.Base(modulePath))))
	}
	for i, store := range stores {
		rel, _ := filepath.Rel(systemClientDir, store.path)
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		b.WriteString(fmt.Sprintf("import * as S%d from %s;\n", i, strconv.Quote(rel)))
	}
	for i, plugin := range plugins {
		rel, _ := filepath.Rel(systemClientDir, plugin.path)
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		b.WriteString(fmt.Sprintf("import * as P%d from %s;\n", i, strconv.Quote(rel)))
	}
	b.WriteString("window._gosh = window._gosh || {}; window._gosh.createStore = createStore; window.__GOSH_PENDING_STORES__ = [];\n")
	if workerURL != "" {
		b.WriteString("window._gosh.siteSearchWorkerURL=" + strconv.Quote(workerURL) + ";\n")
	}
	if len(sharedIDs) > 0 {
		b.WriteString("window.__GOSH_ENTRY_MODULES__ = Object.assign(window.__GOSH_ENTRY_MODULES__ || {}, {")
		for i, id := range sharedIDs {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(id))
			b.WriteByte(':')
			b.WriteString(fmt.Sprintf("M%d", i))
		}
		b.WriteString("});\n")
	}
	for i, store := range stores {
		b.WriteString(fmt.Sprintf("window.__GOSH_PENDING_STORES__.push({name:%s, store:S%d.store, sync:S%d.sync ?? false, session:S%d.session ?? null, setup:S%d.setup});\n", strconv.Quote(store.name), i, i, i, i))
	}
	if len(plugins) == 0 {
		b.WriteString("window._gosh.start();\n")
	} else {
		b.WriteString("Promise.all([")
		for i := range plugins {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(fmt.Sprintf("(typeof P%d.setup==='function'?Promise.resolve(P%d.setup(window._gosh)):Promise.resolve())", i, i))
		}
		b.WriteString("]).then(function(){window._gosh.start();}).catch(function(e){console.error('[_gosh plugin]',e);});\n")
	}
	if err := os.WriteFile(entry, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	nodeModules, _ := filepath.Abs(filepath.Join(systemClientDir, "node_modules"))
	cmd := exec.Command(bin, entry, "--bundle", "--format=iife", "--platform=browser", "--target=es2020", "--minify", "--outfile="+output)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "NODE_PATH="+nodeModules)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("client entry build: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return os.ReadFile(output)
}

func BuildRouteClientChunk(sources map[string]string) ([]byte, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("route client chunk has no modules")
	}
	bin, err := clientToolBinary()
	if err != nil {
		return nil, err
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return nil, fmt.Errorf("resolve route client builder: %w", err)
	}
	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tag := hashText(strings.Join(ids, ","))
	workDir, err := filepath.Abs(sourceFilePath(systemClientDir))
	if err != nil {
		return nil, fmt.Errorf("resolve route client build directory: %w", err)
	}
	entry := filepath.Join(workDir, ".gosh-route-"+tag+".mjs")
	output := filepath.Join(workDir, ".gosh-route-"+tag+".js")
	var b strings.Builder
	for i, id := range ids {
		name := ".gosh-route-" + tag + "-" + strconv.Itoa(i) + ".mjs"
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(sources[id]), 0o644); err != nil {
			return nil, fmt.Errorf("write route client module: %w", err)
		}
		b.WriteString("import * as M" + strconv.Itoa(i) + " from \"./" + name + "\";\n")
	}
	b.WriteString("export const modules={")
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(id) + ":M" + strconv.Itoa(i))
	}
	b.WriteString("};\n")
	if err := os.WriteFile(entry, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(entry)
	defer os.Remove(output)
	for i := range ids {
		defer os.Remove(filepath.Join(workDir, ".gosh-route-"+tag+"-"+strconv.Itoa(i)+".mjs"))
	}
	nodeModules, _ := filepath.Abs(filepath.Join(systemClientDir, "node_modules"))
	cmd := exec.Command(bin, filepath.Base(entry), "--bundle", "--format=esm", "--platform=browser", "--target=es2020", "--minify", "--outfile="+filepath.Base(output))
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "NODE_PATH="+nodeModules)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("route client chunk build: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return os.ReadFile(output)
}
