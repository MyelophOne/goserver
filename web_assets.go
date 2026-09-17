//go:build !myelophone_prod

package goserver

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	logic "github.com/myelophone/goserver/web/runtime"
)

type generatedAsset struct {
	Path string
	Data []byte
}

func compressGzipAsset(data []byte) []byte {
	var out bytes.Buffer
	w, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
	_, _ = w.Write(data)
	_ = w.Close()
	return out.Bytes()
}

func generatedAssetReport(assets []generatedAsset) []logic.BuildAsset {
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	report := make([]logic.BuildAsset, 0, len(assets))
	for _, asset := range assets {
		gzipData := compressGzipAsset(asset.Data)
		report = append(report, logic.BuildAsset{Path: asset.Path, RawSize: len(asset.Data), GzipSize: len(gzipData)})
	}
	return report
}

func writeGeneratedAssetReport(path string, report []logic.BuildAsset) error {
	type assetSize struct {
		Path     string `json:"path"`
		RawSize  string `json:"rawSize"`
		GzipSize string `json:"gzipSize"`
	}
	assets := make([]assetSize, 0, len(report))
	for _, asset := range report {
		assets = append(assets, assetSize{Path: asset.Path, RawSize: formatAssetSize(asset.RawSize), GzipSize: formatAssetSize(asset.GzipSize)})
	}
	data, err := json.MarshalIndent(assets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func formatAssetSize(bytes int) string {
	const unit = 1000
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < unit*unit {
		return fmt.Sprintf("%.1f KB", float64(bytes)/unit)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(unit*unit))
}
