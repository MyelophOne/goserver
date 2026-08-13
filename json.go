package goserver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type pathSegment struct {
	key        string
	index      int
	isIndex    bool
	isWildcard bool
}

var pathCache sync.Map

func BytesToJSON(data []byte) (map[string]any, error) {
	var result map[string]any

	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	err := json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func compilePath(path string) []pathSegment {
	if v, ok := pathCache.Load(path); ok {
		return v.([]pathSegment)
	}

	parts := strings.Split(path, ".")
	segments := make([]pathSegment, 0, len(parts))

	for _, p := range parts {

		if p == "*" {
			segments = append(segments, pathSegment{isWildcard: true})
			continue
		}

		if idx, err := strconv.Atoi(p); err == nil {
			segments = append(segments, pathSegment{
				isIndex: true,
				index:   idx,
			})
			continue
		}

		segments = append(segments, pathSegment{key: p})
	}

	pathCache.Store(path, segments)
	return segments
}

func GetDotPath(m map[string]any, path string) (any, bool) {
	if m == nil || path == "" {
		return nil, false
	}

	segments := compilePath(path)
	var current any = m

	for _, s := range segments {

		switch node := current.(type) {

		case map[string]any:

			if s.isWildcard {
				return node, true
			}

			if s.isIndex {
				return nil, false
			}

			v, ok := node[s.key]
			if !ok {
				return nil, false
			}

			current = v

		case []any:

			if s.isWildcard {
				return node, true
			}

			if s.isIndex {
				if s.index < 0 || s.index >= len(node) {
					return nil, false
				}
				current = node[s.index]
				continue
			}

			return nil, false

		default:
			return nil, false
		}
	}

	return current, true
}
