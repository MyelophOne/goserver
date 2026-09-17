//go:build !myelophone_prod

package goserver

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	logic "github.com/myelophone/goserver/web/runtime"
)

func resolveTailwindBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MYELOPHONE_TAILWIND_BIN")); override != "" {
		path, err := filepath.Abs(override)
		if err == nil {
			if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
				return path, nil
			}
		}
		if lp, lookErr := exec.LookPath(override); lookErr == nil {
			return lp, nil
		}
		return "", fmt.Errorf("MYELOPHONE_TAILWIND_BIN=%q does not point to an executable", override)
	}

	tools, err := webToolchain()
	if err != nil {
		return "", err
	}
	return filepath.Join(tools.Tailwind, "node_modules", "@tailwindcss", "cli", "dist", "index.mjs"), nil
}

func readTailwindGlobalCSS(root string) (string, error) {
	dir := filepath.Join(root, "styles")
	entries, err := sourceReadDir(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var out strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gosh") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := sourceReadFile(path)
		if err != nil {
			return "", err
		}
		source := string(data)
		blocks, parseErr := parseSFCBlocks(source)
		foundStyle := false
		if parseErr == nil {
			for _, block := range blocks {
				if block.Name != "style" {
					continue
				}
				foundStyle = true
				out.WriteString("\n/* global/styles/")
				out.WriteString(entry.Name())
				out.WriteString(" */\n")
				out.WriteString(block.Block.Content)
				out.WriteByte('\n')
			}
		}
		if !foundStyle && strings.TrimSpace(source) != "" {
			out.WriteString("\n/* global/styles/")
			out.WriteString(entry.Name())
			out.WriteString(" */\n")
			out.WriteString(source)
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func compileTailwind(binary, source, tailwindGlobalCSS, systemDefaultCSS, projectDefaultCSS, globalCSS string, minify bool) (string, error) {
	tools, err := webToolchain()
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp(tools.Tailwind, ".myelophone-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	sourcePath := filepath.Join(tmpDir, "reachable.gosh")
	inputPath := filepath.Join(tmpDir, "input.css")
	outputPath := filepath.Join(tmpDir, "output.css")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		return "", err
	}

	input := `@import "tailwindcss" source(none);` + "\n"
	input += `@source "./reachable.gosh";` + "\n"
	if strings.TrimSpace(tailwindGlobalCSS) != "" {
		input += "\n/* web/system/tailwind/global.css */\n" + tailwindGlobalCSS + "\n"
	}
	if strings.TrimSpace(systemDefaultCSS) != "" {
		input += "\n/* web/system/css/default.css */\n" + systemDefaultCSS + "\n"
	}
	if strings.TrimSpace(projectDefaultCSS) != "" {
		input += "\n/* web/css/default.css */\n" + projectDefaultCSS + "\n"
	}
	if strings.TrimSpace(globalCSS) != "" {
		input += "\n/* web/global/styles/*.gosh */\n" + globalCSS + "\n"
	}
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		return "", err
	}

	inputAbs, err := filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return "", err
	}
	args := []string{"-i", inputAbs, "-o", outputAbs}
	if minify {
		args = append(args, "--minify")
	}
	if strings.HasSuffix(binary, ".mjs") || strings.HasSuffix(binary, ".js") {
		args = append([]string{binary}, args...)
		binary, err = resolveNodeBinary()
		if err != nil {
			return "", err
		}
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = tools.Tailwind
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("tailwindcss: %w: %s", err, message)
		}
		return "", fmt.Errorf("tailwindcss: %w", err)
	}
	css, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read Tailwind output: %w", err)
	}
	return processFinalCSS(string(css))
}

func resolveNodeBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MYELOPHONE_NODE_BIN")); override != "" {
		if path, err := filepath.Abs(override); err == nil {
			if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
				return path, nil
			}
		}
		if path, err := exec.LookPath(override); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("MYELOPHONE_NODE_BIN=%q does not point to an executable", override)
	}
	if path, err := exec.LookPath("node"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("final CSS processing requires Node.js; install Node or set MYELOPHONE_NODE_BIN")
}

func processFinalCSS(css string) (string, error) {
	node, err := resolveNodeBinary()
	if err != nil {
		return "", err
	}
	tools, err := webToolchain()
	if err != nil {
		return "", err
	}
	runner := filepath.Join(tools.Tailwind, "postcss-runner.mjs")
	if st, err := os.Stat(runner); err != nil || st.IsDir() {
		return "", fmt.Errorf("final CSS PostCSS runner is missing: %s", runner)
	}
	tmpDir, err := os.MkdirTemp(tools.Tailwind, ".myelophone-postcss-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	input := filepath.Join(tmpDir, "input.css")
	output := filepath.Join(tmpDir, "output.css")
	if err := os.WriteFile(input, []byte(css), 0o600); err != nil {
		return "", err
	}
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(node, runner, inputAbs, outputAbs)
	cmd.Dir = tools.Tailwind
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("final CSS PostCSS: %w: %s", err, message)
		}
		return "", fmt.Errorf("final CSS PostCSS: %w", err)
	}
	processed, err := os.ReadFile(output)
	if err != nil {
		return "", fmt.Errorf("read final PostCSS output: %w", err)
	}
	return string(processed), nil
}

func BuildTailwind(pages *Pages, components, layouts *Components, cfg logic.RuntimeConfig) (TailwindBuild, error) {
	result := TailwindBuild{CSSByPage: map[string]string{}, Sources: map[string][]string{}}
	binary, err := resolveTailwindBinary()
	if err != nil {
		return result, err
	}
	result.Binary = binary
	globalCSS, err := readTailwindGlobalCSS(systemGlobalDir)
	if err != nil {
		return result, fmt.Errorf("tailwind global styles: %w", err)
	}
	tailwindGlobalCSS, err := loadOptionalCSS(systemTailwindGlobal)
	if err != nil {
		return result, fmt.Errorf("tailwind global CSS: %w", err)
	}
	systemDefaultCSS, err := loadOptionalCSS(filepath.Join(systemFrameworkCSSDir, "default.css"))
	if err != nil {
		return result, fmt.Errorf("system default CSS: %w", err)
	}
	projectDefaultCSS, err := loadOptionalCSS(filepath.Join(systemWebCSSDir, "default.css"))
	if err != nil {
		return result, fmt.Errorf("project default CSS: %w", err)
	}

	var combinedSource strings.Builder
	for _, page := range pageList(pages) {
		source, names := tailwindSourceForPage(page, components, layouts, cfg.Render.DefaultLayout)
		result.Sources[page.RelativePath] = names
		combinedSource.WriteString(source)
		combinedSource.WriteByte('\n')
	}
	if combinedSource.Len() == 0 {
		return result, nil
	}
	css, err := compileTailwind(binary, combinedSource.String(), tailwindGlobalCSS, systemDefaultCSS, projectDefaultCSS, globalCSS, cfg.Tailwind.Minify)
	if err != nil {
		return result, fmt.Errorf("compile shared Tailwind CSS: %w", err)
	}
	result.CSSByPage["@shared"] = css
	return result, nil
}

type DependencyGraph struct {
	Components    map[string]bool
	ServerExports map[string]ServerRef
}

func BuildDependencyGraph(pages *Pages, components *Components) *DependencyGraph {
	return BuildDependencyGraphWithLayouts(pages, components, nil, "")
}

func BuildDependencyGraphWithLayouts(pages *Pages, components, layouts *Components, defaultLayout string) *DependencyGraph {
	g := &DependencyGraph{Components: map[string]bool{}, ServerExports: map[string]ServerRef{}}
	seenViews := map[string]bool{}
	var visitView func(*Component)
	var visitNodes func([]Node)
	visitNodes = func(nodes []Node) {
		for _, node := range nodes {
			e, ok := node.(*ElementNode)
			if !ok {
				continue
			}
			if e.IsComponent {
				if c, ok := components.Get(e.Tag); ok && !g.Components[c.Name] {
					g.Components[c.Name] = true
					visitView(c)
				}
			}
			visitNodes(e.Children)
		}
	}
	visitView = func(view *Component) {
		if view == nil || seenViews[view.Path] {
			return
		}
		seenViews[view.Path] = true
		for _, ref := range view.ServerRefs {
			g.ServerExports[ref.Export] = ref
		}
		visitNodes(view.Template)
	}
	for _, p := range pageList(pages) {
		if layouts != nil {
			for _, view := range reachableViewsForPage(p, components, layouts, defaultLayout) {
				visitView(view)
			}
		} else {
			visitView(p.View)
		}
	}
	return g
}

func sortedGraphComponents(g *DependencyGraph) []string {
	out := make([]string, 0, len(g.Components))
	for k := range g.Components {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func tailwindSourceForPage(page *Page, components, layouts *Components, defaultLayout string) (string, []string) {
	views := reachableViewsForPage(page, components, layouts, defaultLayout)
	var b strings.Builder
	names := make([]string, 0, len(views))
	hasClientComponent := false
	for _, view := range views {
		if view.Mode == ModeClient {
			hasClientComponent = true
		}
		name := filepath.ToSlash(view.RelativePath)
		names = append(names, name)
		b.WriteString("\n<!-- GOSH reachable source: ")
		b.WriteString(name)
		b.WriteString(" -->\n")
		b.WriteString(view.Source)
		if !strings.HasSuffix(view.Source, "\n") {
			b.WriteByte('\n')
		}
	}
	if hasClientComponent {
		loaderPath := filepath.Join(systemTemplatesDir, "loader.gosh")
		if data, err := sourceReadFile(loaderPath); err == nil {
			names = append(names, "system/loader.gosh")
			b.WriteString("\n<!-- GOSH system loader -->\n")
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	return b.String(), names
}
