package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type localField struct {
	Path []string `json:"path"`
	Type string   `json:"type"`
}
type localFile struct {
	ID        int          `json:"id"`
	Path      string       `json:"path"`
	Fields    []localField `json:"fields"`
	Link      bool         `json:"link,omitempty"`
	Approved  bool         `json:"-"`
	LinkAlias string       `json:"-"`
}

var privateName = regexp.MustCompile(`(?i)(auth|token|secret|credential|password|keychain|session)`)
var errNoLocal = errors.New("対象範囲に編集可能なローカル設定が見つかりませんでした")

func primitivePaths(value any) []localField {
	fields := []localField{}
	var walk func(any, []string)
	walk = func(value any, path []string) {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if len(fields) >= 100 {
				break
			}
			if privateName.MatchString(key) || key == "__proto__" || key == "constructor" || key == "prototype" {
				continue
			}
			next := append(append([]string{}, path...), key)
			switch child := object[key].(type) {
			case map[string]any:
				walk(child, next)
			case string:
				fields = append(fields, localField{next, "string"})
			case json.Number:
				if numericValue(child) != nil {
					fields = append(fields, localField{next, "number"})
				}
			case bool:
				fields = append(fields, localField{next, "boolean"})
			}
		}
	}
	walk(value, nil)
	return fields
}

func scanLocal() ([]localFile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	roots := []string{filepath.Join(home, ".config"), filepath.Join(home, "Library", "Application Support")}
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
				if path != root && (strings.Count(rel, string(os.PathSeparator)) >= 5 || privateName.MatchString(entry.Name()) || slices.Contains([]string{"node_modules", ".git", "Cache", "Caches"}, entry.Name()) || len(paths) >= 5000) {
					return filepath.SkipDir
				}
				return nil
			}
			if len(paths) < 5000 && (entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0) && !privateName.MatchString(entry.Name()) {
				paths = append(paths, path)
			}
			return nil
		})
	}
	slices.SortFunc(paths, func(a, b string) int {
		// Common config names first even if an application has many unrelated JSON files.
		priority := func(path string) int {
			if strings.Contains(strings.ToLower(filepath.Base(path)), "settings") || strings.Contains(strings.ToLower(filepath.Base(path)), "config") {
				return 0
			}
			return 1
		}
		if priority(a) != priority(b) {
			return priority(a) - priority(b)
		}
		return strings.Compare(a, b)
	})
	files := []localFile{}
	deadline := time.Now().Add(15 * time.Second)
	foreignReads := 0
	for _, path := range paths {
		if len(files) >= 500 || time.Now().After(deadline) {
			break
		}
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			files = append(files, localFile{ID: len(files), Path: path, Link: true})
			continue // The target is outside the approved scope until separately authorized.
		}
		if !info.Mode().IsRegular() || info.Size() > 100_000 || safeLocalFile(path) != nil {
			continue
		}
		file := localFile{ID: len(files), Path: path}
		if fileFormat(path) != "" && (fileFormat(path) == "json" || foreignReads < 100) {
			if fileFormat(path) != "json" {
				foreignReads++
			}
			if data, err := os.ReadFile(path); err == nil && len(data) <= 100_000 {
				if values, err := inspectValues(path, data); err == nil {
					for _, value := range values {
						file.Fields = append(file.Fields, localField{value.Path, value.Type})
					}
				}
			}
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, errNoLocal
	}
	return files, nil
}

func parseLocalJSON(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("JSONオブジェクトではありません")
	}
	var tail any
	if err := decoder.Decode(&tail); err != io.EOF {
		return nil, errors.New("JSONの末尾に不正なデータがあります")
	}
	return value, nil
}
func numericValue(value json.Number) any {
	number, err := strconv.ParseFloat(string(value), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 9007199254740991 {
		return nil
	}
	return number
}

func currentValue(value any, path []string) (any, bool) {
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}
