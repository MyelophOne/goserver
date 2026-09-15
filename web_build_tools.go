package goserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	logic "github.com/myelophone/goserver/web/runtime"
)

type UnusedFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type UnusedReport struct {
	Version  int          `json:"version"`
	Excluded []string     `json:"excluded"`
	Files    []UnusedFile `json:"files"`
}

const unusedReportPath = "tmp/files-unused.json"

var auditAssetExclusions = map[string]bool{
	"assets/robots.txt": true,
}

func BuildUnusedReport(pages *Pages, components, layouts *Components, cfg logic.RuntimeConfig) UnusedReport {
	report := UnusedReport{
		Version: 4,
		Excluded: []string{
			"** (except assets/**)",
		},
	}
	for asset := range auditAssetExclusions {
		report.Excluded = append(report.Excluded, asset)
	}
	sort.Strings(report.Excluded)
	report.Files = append(report.Files, unusedPublicAssetsWithConfig(cfg)...)

	sort.Slice(report.Files, func(i, j int) bool {
		if report.Files[i].Path == report.Files[j].Path {
			return report.Files[i].Kind < report.Files[j].Kind
		}
		return report.Files[i].Path < report.Files[j].Path
	})
	return report
}

var auditAssetReference = regexp.MustCompile(`(?i)(?:^|[\s"'(=])/?assets/([^\s"'<>?#)]+)`)
var auditRootAssetReference = regexp.MustCompile(`(?i)(?:^|[\s"'(=])/([^\s"'<>?#)]+\.[a-z0-9]+)`)

func unusedPublicAssets() []UnusedFile {
	return unusedPublicAssetsWithConfig(logic.RuntimeConfig{})
}

func unusedPublicAssetsWithConfig(cfg logic.RuntimeConfig) []UnusedFile {
	used := map[string]bool{}
	queue := make([]string, 0)
	mark := func(reference string) {
		path := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(reference, "/")))
		if path == "." || strings.HasPrefix(path, "../") || used[path] {
			return
		}
		used[path] = true
		queue = append(queue, path)
	}
	readReferences := func(data []byte) {
		for _, match := range auditAssetReference.FindAllSubmatch(data, -1) {
			mark("assets/" + string(match[1]))
		}
		for _, match := range auditRootAssetReference.FindAllSubmatch(data, -1) {
			mark("assets/" + string(match[1]))
		}
	}

	_ = filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || pathWithin(path, systemPublicDir) {
			if entry != nil && entry.IsDir() && pathWithin(path, systemPublicDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".build-check", ".myelophone-build", ".myelophone-cache", ".task", "dist", "node_modules", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !auditTextFile(path) {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr != nil || info.Size() > 4<<20 {
			return nil
		}
		if data, readErr := os.ReadFile(path); readErr == nil {
			readReferences(data)
		}
		return nil
	})

	for len(queue) > 0 {
		reference := queue[0]
		queue = queue[1:]
		ext := strings.ToLower(filepath.Ext(reference))
		if ext != ".css" && ext != ".js" && ext != ".mjs" {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(systemPublicDir, filepath.FromSlash(strings.TrimPrefix(reference, "assets/")))); err == nil {
			readReferences(data)
		}
	}

	var unused []UnusedFile
	_ = filepath.WalkDir(systemPublicDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		rel, relErr := filepath.Rel(systemPublicDir, path)
		if relErr != nil {
			return nil
		}
		assetPath := "assets/" + filepath.ToSlash(rel)
		if !used[assetPath] && !auditAssetExclusions[assetPath] && !alwaysIncludePublicAsset(assetPath, cfg) {
			unused = append(unused, UnusedFile{
				Path:   assetPath,
				Kind:   "asset",
				Reason: "no static reference found in project sources",
			})
		}
		return nil
	})
	return unused
}

func alwaysIncludePublicAsset(assetPath string, cfg logic.RuntimeConfig) bool {
	base := strings.ToLower(filepath.Base(assetPath))
	if (base == "favicon.ico" || base == "favicon.png" || base == "favicon.svg") && filepath.Dir(assetPath) == "assets" {
		return true
	}
	if configuredPublicAssetPath(cfg.SEO.Image) == assetPath {
		return true
	}
	for _, name := range cfg.Build.IncludeFiles {
		name = filepath.ToSlash(filepath.Clean(strings.TrimPrefix(strings.TrimSpace(name), "/")))
		name = strings.TrimPrefix(name, "assets/")
		if name != "." && !strings.HasPrefix(name, "../") && assetPath == "assets/"+name {
			return true
		}
	}
	return false
}

func configuredPublicAssetPath(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/assets/") {
		return ""
	}
	value = filepath.ToSlash(filepath.Clean(strings.TrimPrefix(value, "/")))
	if strings.HasPrefix(value, "../") || !strings.HasPrefix(value, "assets/") {
		return ""
	}
	return value
}

func auditTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".css", ".go", ".gosh", ".htm", ".html", ".js", ".json", ".md", ".mjs", ".ts", ".tsx", ".vue", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func pathWithin(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func WriteUnusedReport(report UnusedReport) error {
	if len(report.Files) == 0 {
		if err := os.Remove(unusedReportPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove("myelophone-unused.json")
		_ = os.Remove("tmp/myelophone-unused.json")
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(unusedReportPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unusedReportPath, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove("myelophone-unused.json")
	_ = os.Remove("tmp/myelophone-unused.json")
	return nil
}

func PrintUnusedReport(report UnusedReport) {
	if len(report.Files) == 0 {
		fmt.Println("Unused 0 file(s)")
		return
	}
	fmt.Printf("Unused %d file(s); report saved to `%s`\n", len(report.Files), unusedReportPath)
}

func PruneUnused(yes bool) error {
	if !yes {
		return fmt.Errorf("refusing to delete without --yes; review %s first", unusedReportPath)
	}
	data, err := os.ReadFile(unusedReportPath)
	if err != nil {
		return err
	}
	var report UnusedReport
	if err := json.Unmarshal(data, &report); err != nil {
		return err
	}
	allowed := []string{"assets/"}
	for _, f := range report.Files {
		p := sourcePath(f.Path)
		ok := false
		for _, prefix := range allowed {
			if strings.HasPrefix(p, prefix) {
				ok = true
				break
			}
		}
		if !ok || strings.HasPrefix(p, "assets/seo/") {
			return fmt.Errorf("unsafe path in unused report: %s", p)
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("removed", p)
	}
	return nil
}

func optimizePublicImage(src, dst string, cfg logic.RuntimeConfig) (bool, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if !cfg.Images.Optimize || (ext != ".jpg" && ext != ".jpeg" && ext != ".png") {
		return false, nil
	}
	f, err := os.Open(src)
	if err != nil {
		return false, err
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return false, nil
	}
	var buf bytes.Buffer
	if ext == ".png" {
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		err = enc.Encode(&buf, img)
	} else {
		q := cfg.Images.JPEGQuality
		if q <= 0 || q > 100 {
			q = 82
		}
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: q})
	}
	if err != nil {
		return false, err
	}
	original, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	if buf.Len() >= len(original) {
		return false, nil
	}
	return true, os.WriteFile(dst, buf.Bytes(), 0o644)
}

func copyPublicOptimized(src, dst string, cfg logic.RuntimeConfig, unused map[string]bool) error {
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
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != src {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if unused[sourcePath(p)] {
			return nil
		}
		if ok, err := optimizePublicImage(p, target, cfg); err != nil {
			return err
		} else if ok {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		mode := os.FileMode(0o644)
		if info != nil {
			mode = info.Mode().Perm()
		}
		return os.WriteFile(target, data, mode)
	})
}
