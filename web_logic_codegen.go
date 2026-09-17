//go:build !myelophone_prod

package goserver

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const generatedWebDir = "internal/goservergen"

func webProjectModulePath() (string, error) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	modulePattern := regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`)
	match := modulePattern.FindStringSubmatch(string(goMod))
	if len(match) != 2 {
		return "", fmt.Errorf("read module path from go.mod")
	}
	return match[1], nil
}

func GenerateWebLogicBindings() error {
	modulePath, err := webProjectModulePath()
	if err != nil {
		return err
	}
	type logicBinding struct {
		Export, ImportPath string
	}
	bindings := map[string]logicBinding{}
	for _, root := range []string{systemPagesDir, systemComponentsDir, systemLayoutsDir} {
		if err := sourceWalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".gosh" {
				return nil
			}
			data, err := sourceReadFile(path)
			if err != nil {
				return err
			}
			for _, ref := range parseServerRefs(string(data)) {
				dir := filepath.ToSlash(filepath.Dir(filepath.Clean(ref.Path)))
				dir = strings.TrimPrefix(dir, "./")
				if dir == "." || dir == "" {
					return fmt.Errorf("@server %q must point to a Go file in a package directory", ref.Path)
				}
				if !strings.HasPrefix(dir, "web/") {
					dir = "web/" + dir
				}
				binding := logicBinding{Export: ref.Export, ImportPath: sourceModulePath(filepath.Join(dir, filepath.Base(ref.Path)), modulePath) + "/" + dir}
				if previous, exists := bindings[ref.Export]; exists && previous.ImportPath != binding.ImportPath {
					return fmt.Errorf("@server export %q is declared by both %s and %s", ref.Export, previous.ImportPath, binding.ImportPath)
				}
				bindings[ref.Export] = binding
			}
			return nil
		}); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	keys := make([]string, 0, len(bindings))
	for export := range bindings {
		keys = append(keys, export)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("package generated\n")
	if len(keys) > 0 {
		imports := map[string]string{}
		for _, key := range keys {
			imports[bindings[key].ImportPath] = ""
		}
		paths := make([]string, 0, len(imports))
		for path := range imports {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for i, path := range paths {
			imports[path] = fmt.Sprintf("logic%d", i)
		}
		fmt.Fprintf(&b, "\nimport (\n")
		for _, path := range paths {
			fmt.Fprintf(&b, "\t%s %q\n", imports[path], path)
		}
		fmt.Fprintf(&b, "\truntime %q\n)\n\nfunc init() {\n", goserverModulePath+"/web/runtime")
		for _, export := range keys {
			binding := bindings[export]
			fmt.Fprintf(&b, "\truntime.RegisterHandler(%q, %s.%s{})\n", export, imports[binding.ImportPath], export)
		}
		b.WriteString("}\n")
	}
	target := filepath.Join(generatedWebDir, "bindings_gen.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output := []byte(b.String())
	if current, err := os.ReadFile(target); err != nil || string(current) != string(output) {
		if err := os.WriteFile(target, output, 0o644); err != nil {
			return err
		}
	}
	if err := generateWebServerBindings(modulePath); err != nil {
		return err
	}
	_, err = writeWebEntry()
	return err
}

type generatedServerRoute struct {
	Method, Path, Export, ImportPath string
}

func generateWebServerBindings(modulePath string) error {
	var routes []generatedServerRoute
	err := sourceWalkDir("web/server", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == "server.go" {
			return nil
		}
		name := strings.TrimSuffix(entry.Name(), ".go")
		parts := strings.Split(name, ".")
		if len(parts) != 2 {
			return fmt.Errorf("web server file %s must end in .<method>.go", path)
		}
		method := strings.ToUpper(parts[1])
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			return fmt.Errorf("unsupported web server method in %s", path)
		}
		data, err := sourceReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
		if err != nil {
			return err
		}
		export := ""
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.IsExported() {
				export = fn.Name.Name
				break
			}
		}
		if export == "" {
			return fmt.Errorf("web server file %s needs one exported handler function", path)
		}
		rel, _ := filepath.Rel("web/server", path)
		segments := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
		base := parts[0]
		if base != "index" {
			segments = append(segments, base)
		}
		var route []string
		for _, segment := range segments {
			if segment == "." || segment == "" {
				continue
			}
			if strings.HasPrefix(segment, "[...") && strings.HasSuffix(segment, "]") {
				route = append(route, "*"+strings.TrimSuffix(strings.TrimPrefix(segment, "[..."), "]"))
				continue
			}
			if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") {
				route = append(route, ":"+strings.TrimSuffix(strings.TrimPrefix(segment, "["), "]"))
				continue
			}
			route = append(route, segment)
		}
		packageDir := filepath.ToSlash(filepath.Dir(rel))
		if packageDir == "." {
			packageDir = ""
		}
		for _, segment := range strings.Split(packageDir, "/") {
			if strings.ContainsAny(segment, "[]") {
				return fmt.Errorf("web server directory %s is not a valid Go package path; put dynamic segments in the file name (for example users/[id].get.go)", path)
			}
		}
		importPath := sourceModulePath(path, modulePath) + "/web/server"
		if packageDir != "" {
			importPath += "/" + packageDir
		}
		routes = append(routes, generatedServerRoute{Method: method, Path: "/" + strings.Join(route, "/"), Export: export, ImportPath: importPath})
		return nil
	})
	if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Path < routes[j].Path || routes[i].Path == routes[j].Path && routes[i].Method < routes[j].Method
	})
	var b strings.Builder
	b.WriteString("package generated\n")
	if len(routes) > 0 {
		packageDirs := map[string]struct{}{}
		for _, route := range routes {
			packageDirs[route.ImportPath] = struct{}{}
		}
		dirs := make([]string, 0, len(packageDirs))
		for dir := range packageDirs {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		aliases := make(map[string]string, len(dirs))
		for i, dir := range dirs {
			aliases[dir] = fmt.Sprintf("serverpkg%d", i)
		}

		fmt.Fprintf(&b, "\nimport (\n\t\"net/http\"\n")
		for _, dir := range dirs {
			fmt.Fprintf(&b, "\t%s %q\n", aliases[dir], dir)
		}
		fmt.Fprintf(&b, "\truntime %q\n)\n\nfunc init() {\n", goserverModulePath+"/web/runtime")
		for _, route := range routes {
			fmt.Fprintf(&b, "\truntime.RegisterServerRoute(runtime.ServerRoute{Method: http.Method%s, Path: %q, Handler: %s.%s})\n", strings.Title(strings.ToLower(route.Method)), route.Path, aliases[route.ImportPath], route.Export)
		}
		b.WriteString("}\n")
	}
	target := filepath.Join(generatedWebDir, "server_routes_gen.go")
	data := []byte(b.String())
	if current, err := os.ReadFile(target); err == nil && string(current) == string(data) {
		return nil
	}
	return os.WriteFile(target, data, 0o644)
}
