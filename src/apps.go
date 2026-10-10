package main

import (
	"os"
	"path/filepath"
	"strings"
)

// App bundle names may help locate settings, but apps are never write targets.
func scanApps() []localFile {
	home, _ := os.UserHomeDir()
	roots := []string{"/Applications", filepath.Join(home, "Applications")}
	apps := []localFile{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if len(apps) >= 300 {
				return apps
			}
			if !entry.IsDir() || privateName.MatchString(entry.Name()) || !strings.EqualFold(filepath.Ext(entry.Name()), ".app") {
				continue
			}
			apps = append(apps, localFile{ID: len(apps), Path: filepath.Join(root, entry.Name()), Fields: []localField{}})
		}
	}
	return apps
}
