package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var releaseTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

func main() {
	fmt.Println(currentVersion())
}

func currentVersion() string {
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "dev"
	}
	commit := strings.TrimSpace(string(head))
	var versions []string
	if output, err := exec.Command("git", "tag", "--points-at", "HEAD").Output(); err == nil {
		versions = append(versions, strings.Fields(string(output))...)
	}
	if output, err := exec.Command("go", "env", "GOMODCACHE").Output(); err == nil {
		pattern := filepath.Join(strings.TrimSpace(string(output)), "cache", "download", "github.com", "myelophone", "goserver", "@v", "*.info")
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			var info struct {
				Version string `json:"Version"`
				Origin  struct {
					Hash string `json:"Hash"`
				} `json:"Origin"`
			}
			if json.Unmarshal(data, &info) == nil && info.Origin.Hash == commit {
				versions = append(versions, info.Version)
			}
		}
	}
	best := "dev"
	for _, version := range versions {
		if releaseTag.MatchString(version) && (best == "dev" || compareRelease(version, best) > 0) {
			best = version
		}
	}
	return best
}

func compareRelease(a, b string) int {
	aParts := releaseTag.FindStringSubmatch(a)
	bParts := releaseTag.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		aNumber, _ := strconv.Atoi(aParts[i])
		bNumber, _ := strconv.Atoi(bParts[i])
		if aNumber < bNumber {
			return -1
		}
		if aNumber > bNumber {
			return 1
		}
	}
	return 0
}
