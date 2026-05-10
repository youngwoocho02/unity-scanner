package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const checkInterval = 1 * time.Hour

var fetchLatestReleaseFn = fetchLatestRelease

type versionCache struct {
	CheckedAt int64  `json:"checked_at"`
	Latest    string `json:"latest,omitempty"`
	Outdated  bool   `json:"outdated,omitempty"`
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".unity-scanner", "version-check.json")
}

func loadCache(path string) (*versionCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cache versionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveCache(path string, cache *versionCache) {
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0755)
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

func printUpdateNotice() {
	if Version == "dev" {
		return
	}

	path := cacheFilePath()
	if path == "" {
		return
	}

	now := time.Now().Unix()
	cache, _ := loadCache(path)
	latestNotice := ""

	if cache != nil && cache.Outdated && cache.Latest != "" && cache.Latest != Version {
		latestNotice = cache.Latest
	}

	if cache != nil && now-cache.CheckedAt < int64(checkInterval.Seconds()) {
		if latestNotice != "" {
			printNotice(Version, latestNotice)
		}
		return
	}

	release, err := fetchLatestReleaseFn()
	if err != nil {
		if cache != nil {
			cache.CheckedAt = now
			saveCache(path, cache)
		} else {
			saveCache(path, &versionCache{CheckedAt: now})
		}
		if latestNotice != "" {
			printNotice(Version, latestNotice)
		}
		return
	}

	nextCache := &versionCache{
		CheckedAt: now,
		Latest:    release.TagName,
		Outdated:  release.TagName != "" && release.TagName != Version,
	}
	saveCache(path, nextCache)

	if nextCache.Outdated {
		latestNotice = release.TagName
	} else {
		latestNotice = ""
	}

	if latestNotice != "" {
		printNotice(Version, latestNotice)
	}
}

func printNotice(current, latest string) {
	fmt.Fprintf(os.Stderr, "\nUpdate available: %s -> %s\nRun \"unity-scanner update\" to upgrade.\n", current, latest)
}
