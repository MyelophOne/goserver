//go:build !myelophone_prod

package goserver

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestProductionBuildFromExternalModule(t *testing.T) {
	frameworkRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	t.Chdir(projectRoot)
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", fmt.Sprintf("module example.com/consumer\n\ngo 1.27.1\n\nrequire github.com/myelophone/goserver v0.0.0\n\nreplace github.com/myelophone/goserver => %s\n", goQuote(filepath.ToSlash(frameworkRoot))))
	checksums, err := os.ReadFile(filepath.Join(frameworkRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	write("go.sum", string(checksums))
	write("web/pages/index.gosh", "<!-- @layout none -->\n<!-- @server ./logic/data.go#ConsumerPage -->\n<template><h1>{{ message }}</h1></template>")
	write("web/components/Probe.gosh", "<template><span>probe</span></template>")
	write("web/logic/data.go", `package logic
import runtime "github.com/myelophone/goserver/web/runtime"
type ConsumerPage struct { runtime.Noop }
func (ConsumerPage) Render(*runtime.Context, runtime.Props) (runtime.Data, error) { return runtime.Data{"message": "consumer-page"}, nil }
`)
	write("assets/icon.svg", "<svg>consumer-icon</svg>")
	write("assets/custom.txt", "consumer-asset")
	if err := copyPublicOptimized("assets", filepath.Join("dist", "assets"), logic.RuntimeConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"favicon.ico", "icon.svg", "apple-touch-icon.png", "myelophone_eng.png", "seo/goserver-cat.jpg"} {
		if _, err := os.Stat(filepath.Join("dist", "assets", filepath.FromSlash(name))); err != nil {
			t.Fatalf("inherited consumer asset %s: %v", name, err)
		}
	}
	write("cmd/web_cli.go", `//go:build webcli

package main
import (
 "log"
 "os"
 goserver "github.com/myelophone/goserver"
)
func main() { if err := goserver.RunWebCLI(os.Args[1:]); err != nil { log.Fatal(err) } }
`)
	generate := exec.Command("go", "run", "-mod=mod", "-tags", "webcli", "./cmd", "generate")
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap consumer CLI: %v\n%s", err, output)
	}
	entry, err := os.ReadFile("cmd/web_import_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "//go:build !webcli || webbuild") {
		t.Fatalf("generated entry does not load handlers for the production builder: %s", entry)
	}
	_, err = writeProductionEmbed([]string{"web/pages/index.gosh", "web/components/Probe.gosh"}, logic.ProductionBundle{
		TailwindCSS: map[string]string{"index.gosh": "h1{color:red}"},
		FinalCSS:    map[string]string{hashText(systemBaseCSS): minifyCSS(systemBaseCSS)},
		StyleAssets: map[string]string{"consumer-style": "h1{color:red}"},
		PageStyles:  map[string][]string{"index.gosh": {"/_gosh/style/consumer-style.css"}},
		ClientEntry: []byte("consumer-client"), RouteChunks: map[string][]byte{"chunk": []byte("consumer-chunk")},
		RouteMaps: map[string]map[string]string{"index.gosh": {"source": "chunk"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := logic.RuntimeConfig{}
	cfg.Runtime.Enabled = true
	cfg.Render.DefaultLayout = "none"
	cfg.SEO.Title = "Consumer title"
	if _, err := writeProductionRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	write("cmd/main.go", `//go:build !webcli

package main
import (
 "fmt"
 "io/fs"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 goserver "github.com/myelophone/goserver"
 runtime "github.com/myelophone/goserver/web/runtime"
)
func main() {
 bundle, ok := runtime.UseProductionBundle()
 if !ok || string(bundle.ClientEntry) != "consumer-client" || string(bundle.RouteChunks["chunk"]) != "consumer-chunk" || bundle.TailwindCSS["index.gosh"] != "h1{color:red}" { panic("invalid production bundle") }
 if bundle.StyleAssets["consumer-style"] != "h1{color:red}" || len(bundle.PageStyles["index.gosh"]) != 1 || bundle.PageStyles["index.gosh"][0] != "/_gosh/style/consumer-style.css" { panic("invalid production styles") }
 page, err := fs.ReadFile(bundle.Sources, "web/pages/index.gosh")
 if err != nil || !strings.Contains(string(page), "message") { panic("missing embedded page") }
 if _, err := fs.ReadFile(bundle.Sources, "assets/robots.txt"); err != nil { panic("missing embedded robots") }
 if _, err := fs.ReadFile(bundle.Sources, "assets/icon.svg"); err == nil { panic("embedded public image") }
 cfg, err := runtime.UseRuntimeConfig()
 if err != nil || cfg.SEO.Title != "Consumer title" { panic("missing embedded config") }
 pages, err := goserver.LoadPages("web/pages")
 if err != nil || pages.Len() != 1 { panic(fmt.Sprintf("pages: %v", err)) }
 handler, ok := runtime.Resolve("ConsumerPage")
 if !ok { panic("missing consumer handler") }
 data, err := handler.Render(&runtime.Context{}, runtime.Props{})
 if err != nil || data["message"] != "consumer-page" { panic("invalid consumer handler") }
 server := goserver.NewServer("0")
 if err := server.EnableWeb(); err != nil { panic(err) }
 assets := server.StaticAssetsMiddleware(http.NotFoundHandler())
 for _, path := range []string{"/assets/myelophone_eng.png", "/assets/myelophone_eng_white.png", "/assets/seo/goserver-cat.jpg", "/favicon.ico", "/icon.svg", "/assets/icon.svg", "/apple-touch-icon.png", "/assets/custom.txt", "/custom.txt"} {
  for _, method := range []string{http.MethodGet, http.MethodHead} {
   response := httptest.NewRecorder()
   request := httptest.NewRequest(method, path, nil)
   assets.ServeHTTP(response, request)
   if response.Code != http.StatusOK || (method == http.MethodGet && response.Body.Len() == 0) { panic(fmt.Sprintf("asset %s %s: %d", method, path, response.Code)) }
   if request.URL.Path != path { panic("request URL mutated") }
   if method == http.MethodGet && strings.HasSuffix(path, "icon.svg") && response.Body.String() != "<svg>consumer-icon</svg>" { panic("distribution override shadowed by working directory") }
   if method == http.MethodGet && strings.HasSuffix(path, "custom.txt") && response.Body.String() != "consumer-asset" { panic("consumer asset missing") }
  }
 }
 executable, err := os.Executable()
 if err != nil { panic(err) }
 iconPath := filepath.Join(filepath.Dir(executable), "assets", "icon.svg")
 originalIcon, err := os.ReadFile(iconPath)
 if err != nil { panic(err) }
 if err := os.Remove(iconPath); err != nil { panic(err) }
 response := httptest.NewRecorder()
 assets.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/icon.svg", nil))
 if response.Code != http.StatusNotFound { panic("image still served without disk file") }
 if response.Header().Get("Cache-Control") != "no-store" { panic("missing image response is cacheable") }
 if err := os.WriteFile(iconPath, []byte("disk-override"), 0644); err != nil { panic(err) }
 response = httptest.NewRecorder()
 assets.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/icon.svg", nil))
 if response.Code != http.StatusOK || response.Body.String() != "disk-override" { panic("image not served from disk") }
 if err := os.WriteFile(iconPath, originalIcon, 0644); err != nil { panic(err) }
 response = httptest.NewRecorder()
 assets.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
 if response.Code != http.StatusOK { panic("embedded robots unavailable") }
 fmt.Println("consumer production ready")
}
`)
	if _, err := writeWebEntry(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("web/system"); !os.IsNotExist(err) {
		t.Fatalf("consumer build created web/system: %v", err)
	}
	bin := filepath.Join(projectRoot, "dist", "consumer")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-mod=mod", "-tags", "myelophone_prod", "-trimpath", "-o", bin, "./cmd")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("external build: %v\n%s", err, output)
	}
	productionBinary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"web/system/client/.yarn/releases/yarn-4.18.0.cjs",
		"web/system/tailwind/.yarn/releases/yarn-4.18.0.cjs",
	} {
		tool, err := os.ReadFile(filepath.Join(frameworkRoot, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(productionBinary, tool[:256]) {
			t.Fatalf("production binary contains build tool %s", path)
		}
	}
	symbols := exec.Command("go", "tool", "nm", bin)
	output, err := symbols.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect production symbols: %v\n%s", err, output)
	}
	for _, name := range []string{"webToolchain", "compileTailwind", "processFinalCSS", "BuildClientEntry", "BuildRouteClientChunk", "GenerateWebLogicBindings", "RunWebCLI", "buildProduction", "materializeClientLayers"} {
		if bytes.Contains(output, []byte("github.com/myelophone/goserver."+name)) {
			t.Fatalf("production binary contains build function %s", name)
		}
	}
	if bytes.Contains(output, []byte("testing/fstest.")) {
		t.Fatal("production bundle depends on testing/fstest")
	}
	write("assets/icon.svg", "source-only")
	unrelated := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unrelated, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "assets", "icon.svg"), []byte("unrelated-asset"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{projectRoot, filepath.Join(projectRoot, "dist"), unrelated} {
		run := exec.Command(bin)
		run.Dir = cwd
		run.Env = append(os.Environ(), "APP_ENV=prod", "MYELOPHONE_WEB_ENABLED=true", "PATH=")
		if output, err := run.CombinedOutput(); err != nil || !strings.Contains(string(output), "consumer production ready") {
			t.Fatalf("standalone consumer from %s: %v\n%s", cwd, err, output)
		}
	}
	if err := os.RemoveAll(generatedWebDir); err != nil {
		t.Fatal(err)
	}
	generate = exec.Command("go", "run", "-mod=mod", "-tags", "webcli", "./cmd", "generate")
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("regenerate without generated package: %v\n%s", err, output)
	}
}
