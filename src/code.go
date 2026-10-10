package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Only selected tool directories are scanned. No installed code is executed.
func scanCodePaths(roots []string) []string {
	paths := []string{}
	for _, root := range roots {
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if entry.IsDir() {
				if path != root && (strings.Count(rel, string(os.PathSeparator)) >= 5 || privateName.MatchString(entry.Name()) || slices.Contains([]string{"node_modules", ".git", "Caches", "Cache"}, entry.Name()) || len(paths) >= 1000) {
					return filepath.SkipDir
				}
				return nil
			}
			if len(paths) >= 1000 || !entry.Type().IsRegular() || privateName.MatchString(entry.Name()) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !slices.Contains([]string{".js", ".mjs", ".cjs", ".ts", ".go", ".rs", ".py", ".lua", ".md"}, ext) && !(ext == ".json" && strings.Contains(strings.ToLower(entry.Name()), "schema")) {
				return nil
			}
			info, err := entry.Info()
			if err == nil && info.Size() <= 50_000 {
				paths = append(paths, path)
			}
			return nil
		})
	}
	slices.Sort(paths)
	return paths
}
