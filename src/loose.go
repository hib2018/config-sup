package main

import (
	"bytes"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// A conservative fallback for unquoted, single-line key = value settings.
// Other syntaxes are displayed only if a future verifier can edit them safely.
var plainKey = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type plainEntry struct {
	key, value string
	start, end int
}

func plainEntries(data []byte) []plainEntry {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	entries := []plainEntry{}
	counts := map[string]int{}
	offset := 0
	for _, line := range strings.SplitAfter(string(data), "\n") {
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		idx := strings.IndexByte(text, '=')
		if idx >= 0 {
			key := strings.TrimSpace(text[:idx])
			if plainKey.MatchString(key) && !privateName.MatchString(key) {
				rest := text[idx+1:]
				start := len(rest) - len(strings.TrimLeft(rest, " \t"))
				rest = rest[start:]
				end := len(rest)
				for _, marker := range []string{" #", "\t#"} {
					if pos := strings.Index(rest, marker); pos >= 0 && pos < end {
						end = pos
					}
				}
				end = len(strings.TrimRight(rest[:end], " \t"))
				value := rest[:end]
				if value != "" && !strings.HasPrefix(value, "\"") && !strings.HasPrefix(value, "'") {
					counts[key]++
					entries = append(entries, plainEntry{key, value, offset + idx + 1 + start, offset + idx + 1 + start + end})
				}
			}
		}
		offset += len(line)
	}
	result := []plainEntry{}
	for _, entry := range entries {
		if counts[entry.key] == 1 && len(result) < 100 {
			result = append(result, entry)
		}
	}
	return result
}
func inspectPlain(data []byte) []localValue {
	values := []localValue{}
	for _, entry := range plainEntries(data) {
		values = append(values, localValue{Path: []string{entry.key}, Type: "string", Value: entry.value})
	}
	return values
}
func patchPlain(data []byte, changes []formatChange) ([]byte, error) {
	entries := plainEntries(data)
	type edit struct {
		start, end int
		value      string
	}
	edits := []edit{}
	for _, change := range changes {
		value, ok := change.Value.(string)
		if !ok || len(change.Path) != 1 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New("単一行の文字列のみ変更できます")
		}
		matched := false
		for _, entry := range entries {
			if entry.key == change.Path[0] {
				for _, old := range edits {
					if old.start == entry.start {
						return nil, errors.New("同じ項目を複数回変更できません")
					}
				}
				edits = append(edits, edit{entry.start, entry.end, value})
				matched = true
				break
			}
		}
		if !matched {
			return nil, errors.New("一意な設定項目が見つかりません")
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	result := append([]byte(nil), data...)
	for _, edit := range edits {
		result = append(append(append([]byte{}, result[:edit.start]...), []byte(edit.value)...), result[edit.end:]...)
	}
	if len(result) > 100_000 {
		return nil, errors.New("変更後のファイルが100KBを超えます")
	}
	for _, change := range changes {
		found := false
		for _, field := range inspectPlain(result) {
			if len(field.Path) == 1 && field.Path[0] == change.Path[0] && field.Value == change.Value {
				found = true
			}
		}
		if !found {
			return nil, errors.New("変更後の設定項目を確認できません")
		}
	}
	return result, nil
}
