package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type localValue struct {
	Path  []string `json:"path"`
	Type  string   `json:"type"`
	Value any      `json:"value"`
}
type formatChange struct {
	Path  []string `json:"path"`
	Value any      `json:"value"`
}

func fileFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	}
	return ""
}
func formatScript(input any, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	script := "src/local-format.mjs"
	if _, err := os.Stat(script); err != nil {
		script = "local-format.mjs"
	} // go test runs inside src/.
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = bytes.NewReader(payload)
	data, err := cmd.Output()
	if err != nil {
		return errors.New("YAML/TOML の解析または変更に失敗しました（未対応の構文や形式を確認してください）")
	}
	if err := json.Unmarshal(data, output); err != nil {
		return err
	}
	return nil
}
func inspectValues(path string, data []byte) ([]localValue, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("UTF-8でない設定ファイルです")
	}
	format := fileFormat(path)
	if format == "json" {
		value, err := parseLocalJSON(data)
		if err != nil {
			return nil, err
		}
		fields := []localValue{}
		for _, field := range primitivePaths(value) {
			current, _ := currentValue(value, field.Path)
			if number, ok := current.(json.Number); ok {
				current = numericValue(number)
			}
			fields = append(fields, localValue{field.Path, field.Type, current})
		}
		return fields, nil
	}
	if format == "" {
		return nil, errors.New("未対応の形式です")
	}
	var fields []localValue
	err := formatScript(map[string]any{"action": "inspect", "format": format, "text": string(data)}, &fields)
	if err != nil {
		return nil, err
	}
	if len(fields) > 100 {
		fields = fields[:100]
	}
	return fields, nil
}
func localValueAt(fields []localValue, path []string) (any, string, bool) {
	for _, field := range fields {
		if reflect.DeepEqual(field.Path, path) {
			return field.Value, field.Type, true
		}
	}
	return nil, "", false
}
func patchForeign(path string, original []byte, changes []formatChange) ([]byte, error) {
	var text string
	if err := formatScript(map[string]any{"action": "patch", "format": fileFormat(path), "text": string(original), "changes": changes}, &text); err != nil {
		return nil, err
	}
	if len(text) > 100_000 {
		return nil, fmt.Errorf("変更後のファイルが100KBを超えます")
	}
	return []byte(text), nil
}
