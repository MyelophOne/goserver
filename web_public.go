package goserver

import (
	"errors"
	"io/fs"
	"sort"
)

type publicLayerFS struct {
	base  fs.FS
	local fs.FS
}

func (f publicLayerFS) Open(name string) (fs.File, error) {
	file, err := f.local.Open(name)
	if !errors.Is(err, fs.ErrNotExist) {
		return file, err
	}
	return f.base.Open(name)
}

func (f publicLayerFS) ReadDir(name string) ([]fs.DirEntry, error) {
	local, err := fs.ReadDir(f.local, name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	base, baseErr := fs.ReadDir(f.base, name)
	if err != nil && baseErr != nil {
		return nil, err
	}
	entries := map[string]fs.DirEntry{}
	for _, entry := range base {
		entries[entry.Name()] = entry
	}
	for _, entry := range local {
		entries[entry.Name()] = entry
	}
	result := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result, nil
}
