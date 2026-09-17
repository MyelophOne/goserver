//go:build !myelophone_prod

package goserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var webToolsMu sync.Mutex

type webToolDirs struct {
	Client   string
	Tailwind string
}

func webToolchain() (webToolDirs, error) {
	webToolsMu.Lock()
	defer webToolsMu.Unlock()
	files := map[string][]byte{}
	tools := publicLayerFS{base: embeddedWebTools, local: os.DirFS(".")}
	hash := sha256.New()
	hash.Write([]byte(runtime.GOOS + "/" + runtime.GOARCH + "\x00"))
	for _, kind := range []string{"client", "tailwind"} {
		root := "web/system/" + kind
		err := fs.WalkDir(tools, root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".myelophone") || entry.Name() == ".cache" {
					return fs.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".myelophone") || strings.HasPrefix(name, ".gosh-") {
				return nil
			}
			if name != "yarn.lock" && name != ".yarnrc.yml" && !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".mjs") && !strings.HasSuffix(name, ".cjs") {
				return nil
			}
			data, err := fs.ReadFile(tools, path)
			if err != nil {
				return err
			}
			relative := kind + strings.TrimPrefix(path, root)
			files[relative] = data
			hash.Write([]byte(relative + "\x00"))
			hash.Write(data)
			return nil
		})
		if err != nil {
			return webToolDirs{}, err
		}
	}
	cacheRoot := os.Getenv("MYELOPHONE_TOOLCHAIN_CACHE")
	if cacheRoot == "" {
		var err error
		cacheRoot, err = os.UserCacheDir()
		if err != nil {
			return webToolDirs{}, err
		}
		cacheRoot = filepath.Join(cacheRoot, "myelophone", "goserver", "toolchains")
	}
	cacheRoot, err := filepath.Abs(cacheRoot)
	if err != nil {
		return webToolDirs{}, err
	}
	root := filepath.Join(cacheRoot, hex.EncodeToString(hash.Sum(nil)))
	dirs := webToolDirs{Client: filepath.Join(root, "client"), Tailwind: filepath.Join(root, "tailwind")}
	ready := func() bool {
		for _, path := range []string{filepath.Join(root, ".ready"), filepath.Join(dirs.Client, "node_modules", "esbuild", "package.json"), filepath.Join(dirs.Tailwind, "node_modules", "postcss", "package.json"), filepath.Join(dirs.Tailwind, "node_modules", "tailwindcss", "package.json")} {
			if _, err := os.Stat(path); err != nil {
				return false
			}
		}
		return true
	}
	if ready() {
		return dirs, nil
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return webToolDirs{}, err
	}
	lock := root + ".lock"
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := os.Mkdir(lock, 0o755); err == nil {
			break
		} else if !os.IsExist(err) {
			return webToolDirs{}, err
		}
		if ready() {
			return dirs, nil
		}
		if time.Now().After(deadline) {
			return webToolDirs{}, fmt.Errorf("web toolchain setup is locked: %s", lock)
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer os.Remove(lock)
	if ready() {
		return dirs, nil
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return webToolDirs{}, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return webToolDirs{}, err
		}
	}
	node, err := resolveNodeBinary()
	if err != nil {
		return webToolDirs{}, err
	}
	for _, dir := range []string{dirs.Client, dirs.Tailwind} {
		yarn := filepath.Join(dir, ".yarn", "releases", "yarn-4.18.0.cjs")
		cmd := exec.Command(node, yarn, "install", "--immutable")
		cmd.Dir = dir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return webToolDirs{}, fmt.Errorf("install web toolchain in %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".ready"), nil, 0o644); err != nil {
		return webToolDirs{}, err
	}
	return dirs, nil
}

func SetupWebTools() error {
	_, err := webToolchain()
	return err
}

func webBuildTemp(pattern string) (string, error) {
	root := filepath.Join("tmp", "goserver", "build")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, pattern)
}
