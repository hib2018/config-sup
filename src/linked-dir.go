package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Scan only safe regular files under the resolved local link target.
func scanLinkedDirectory(root string) ([]localFile, error) {
	if err := safeApprovedDirectory(root); err != nil {
		return nil, err
	}
	paths := []string{}
	deadline := time.Now().Add(15 * time.Second)
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || time.Now().After(deadline) || len(paths) >= 500 {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, path)
		if entry.IsDir() {
			if path != root && (strings.Count(rel, string(os.PathSeparator)) >= 5 || privateName.MatchString(entry.Name()) || slices.Contains([]string{"node_modules", ".git", "Cache", "Caches"}, entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || privateName.MatchString(entry.Name()) || strings.HasPrefix(entry.Name(), ".config-sup-") {
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.Size() <= 100_000 {
			if data, err := os.ReadFile(path); err == nil && !sensitiveContent(data) {
				paths = append(paths, path)
			}
		}
		return nil
	})
	slices.SortFunc(paths, func(a, b string) int {
		priority := func(path string) int {
			name := strings.ToLower(filepath.Base(path))
			if strings.Contains(name, "config") || strings.Contains(name, "settings") || name == "init.lua" {
				return 0
			}
			return 1
		}
		if priority(a) != priority(b) {
			return priority(a) - priority(b)
		}
		return strings.Compare(a, b)
	})
	result := make([]localFile, 0, len(paths))
	for _, path := range paths {
		result = append(result, localFile{ID: len(result), Path: path})
	}
	return result, nil
}
