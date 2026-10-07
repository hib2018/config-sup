package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type binding struct {
	File string
	Key  []string
	Type string
}
type pendingChange struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}
type previewState struct {
	ID    string
	Files map[string][]byte
}
type plan struct {
	mu       sync.Mutex
	Token    string
	Created  time.Time
	Bindings map[string]binding
	Original map[string][]byte
	Pending  *previewState
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func safeLocalFile(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	roots := []string{filepath.Join(home, ".config"), filepath.Join(home, "Library", "Application Support")}
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		for _, ancestor := range []string{home, filepath.Dir(root)} {
			info, err := os.Lstat(ancestor)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("設定ディレクトリにシンボリックリンクが含まれます")
			}
		}
		current := root
		parts := append([]string{""}, strings.Split(rel, string(os.PathSeparator))...)
		for index, part := range parts {
			if part != "" {
				current = filepath.Join(current, part)
			}
			info, err := os.Lstat(current)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("対象にシンボリックリンクが含まれます")
			}
			if index == len(parts)-1 && !info.Mode().IsRegular() {
				return errors.New("通常ファイルではありません")
			}
		}
		return nil
	}
	return errors.New("対象が許可された設定ディレクトリ外です")
}

func preparePlan(repo string, files []source, descriptions map[string]string, local []localFile) (any, *plan, error) {
	token, err := newToken()
	if err != nil {
		return nil, nil, err
	}
	p := &plan{Token: token, Created: time.Now(), Bindings: map[string]binding{}, Original: map[string][]byte{}}
	seenTargets := map[string]bool{}
	for fi := range files {
		for ki := range files[fi].Fields {
			field := &files[fi].Fields[ki]
			if field.Target == nil || field.Target.ID < 0 || field.Target.ID >= len(local) {
				field.Target = nil
				continue
			}
			candidate := local[field.Target.ID]
			targetKey := candidate.Path + "\x00" + strings.Join(field.Target.Path, "\x00")
			if seenTargets[targetKey] || !slicesEqualField(candidate.Fields, field.Target.Path, field.Type) || safeLocalFile(candidate.Path) != nil {
				field.Target = nil
				continue
			}
			data, ok := p.Original[candidate.Path]
			if !ok {
				data, err = os.ReadFile(candidate.Path)
				if err != nil || len(data) > 100_000 {
					field.Target = nil
					continue
				}
				p.Original[candidate.Path] = data
			}
			value, parseErr := parseLocalJSON(data)
			if parseErr != nil {
				field.Target = nil
				continue
			}
			current, ok := currentValue(value, field.Target.Path)
			if !ok || valueType(current) != field.Type {
				field.Target = nil
				continue
			}
			if number, ok := current.(json.Number); ok && numericValue(number) == nil {
				field.Target = nil
				continue
			}
			id := fmt.Sprintf("%d:%d", fi, ki)
			p.Bindings[id] = binding{candidate.Path, field.Target.Path, field.Type}
			seenTargets[targetKey] = true
			if number, ok := current.(json.Number); ok {
				field.Value = numericValue(number)
			} else {
				field.Value = current
			} // Show actual local value, not a model guess.
			field.Target.File = candidate.Path
		}
	}
	if len(p.Bindings) == 0 {
		return nil, nil, errors.New("一致するローカルJSON設定項目が見つかりませんでした。候補は表示できますが適用できません")
	}
	return struct {
		Repo         string            `json:"repo"`
		Files        []source          `json:"files"`
		Descriptions map[string]string `json:"descriptions"`
		Agent        string            `json:"agent"`
		Token        string            `json:"token"`
	}{repo, files, descriptions, "AIによる説明（要確認）", token}, p, nil
}

func slicesEqualField(fields []localField, path []string, typ string) bool {
	for _, f := range fields {
		if reflect.DeepEqual(f.Path, path) && f.Type == typ {
			return true
		}
	}
	return false
}
func valueType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "boolean"
	}
	return ""
}
func setLocal(object map[string]any, path []string, value any) {
	current := object
	for _, key := range path[:len(path)-1] {
		current = current[key].(map[string]any)
	}
	current[path[len(path)-1]] = value
}

func (p *plan) preview(changes []pendingChange) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Pending = nil
	if len(changes) == 0 || len(changes) > len(p.Bindings) {
		return "", "", errors.New("変更項目がありません")
	}
	updated := map[string]map[string]any{}
	modified := map[string]bool{}
	seen := map[string]bool{}
	lines := []string{}
	for _, change := range changes {
		binding, ok := p.Bindings[change.ID]
		if !ok || seen[change.ID] || valueType(change.Value) != binding.Type {
			return "", "", errors.New("無効な変更項目です")
		}
		seen[change.ID] = true
		object, ok := updated[binding.File]
		if !ok {
			if err := safeLocalFile(binding.File); err != nil {
				return "", "", err
			}
			current, err := os.ReadFile(binding.File)
			if err != nil || !bytes.Equal(current, p.Original[binding.File]) {
				return "", "", errors.New("適用先が変更されています。再解析してください")
			}
			object, err = parseLocalJSON(current)
			if err != nil {
				return "", "", errors.New("対象JSONが壊れています")
			}
			updated[binding.File] = object
		}
		before, exists := currentValue(object, binding.Key)
		if !exists || valueType(before) != binding.Type {
			return "", "", errors.New("対象のキーが変更されています")
		}
		if number, ok := before.(json.Number); ok {
			if numericValue(number) == change.Value {
				continue
			}
		} else if reflect.DeepEqual(before, change.Value) {
			continue
		}
		setLocal(object, binding.Key, change.Value)
		modified[binding.File] = true
		lines = append(lines, fmt.Sprintf("%s → %s: %s → %s", binding.File, strings.Join(binding.Key, "."), printable(before), printable(change.Value)))
	}
	if len(lines) == 0 {
		return "", "", errors.New("変更がありません")
	}
	files := map[string][]byte{}
	for path, object := range updated {
		if !modified[path] {
			continue
		}
		text, err := json.MarshalIndent(object, "", "  ")
		if err != nil {
			return "", "", err
		}
		files[path] = append(text, '\n')
	}
	id, err := newToken()
	if err != nil {
		return "", "", err
	}
	p.Pending = &previewState{id, files}
	return id, strings.Join(lines, "\n") + "\n\n※ 対象のJSONファイル全体を整形して書き直します。", nil
}
func printable(value any) string { text, _ := json.Marshal(value); return string(text) }

func (p *plan) apply(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Pending == nil || p.Pending.ID != id {
		return errors.New("差分を確認し直してください")
	}
	pending := p.Pending
	p.Pending = nil
	type staged struct {
		path, temporary string
		data            []byte
	}
	stages := []staged{}
	defer func() {
		for _, stage := range stages {
			os.Remove(stage.temporary)
		}
	}()
	for path, data := range pending.Files {
		if err := safeLocalFile(path); err != nil {
			return err
		}
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, p.Original[path]) {
			return errors.New("適用先が変更されています。再解析してください")
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".config-sup-*")
		if err != nil {
			return err
		}
		stages = append(stages, staged{path, file.Name(), data})
		if _, err = file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err = file.Chmod(info.Mode().Perm()); err != nil {
			file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
	}
	slices.SortFunc(stages, func(a, b staged) int { return strings.Compare(a.path, b.path) })
	for index, stage := range stages {
		if err := safeLocalFile(stage.path); err != nil {
			return fmt.Errorf("%dファイル適用後に失敗しました: %w", index, err)
		}
		current, err := os.ReadFile(stage.path)
		if err != nil || !bytes.Equal(current, p.Original[stage.path]) {
			return fmt.Errorf("%dファイル適用後に対象が変更されました。再解析してください", index)
		}
		if err := os.Rename(stage.temporary, stage.path); err != nil {
			return fmt.Errorf("%dファイル適用後に失敗しました: %w", index, err)
		}
		p.Original[stage.path] = stage.data
	}
	return nil
}
