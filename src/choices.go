package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"
)

func findChoices(paths []string, item localValue, request string) ([]any, error) {
	payload, err := json.Marshal(map[string]any{"paths": paths, "item": map[string]any{"path": item.Path, "type": item.Type}, "request": request})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	script := "src/choices-agent.mjs"
	if _, err := os.Stat(script); err != nil {
		script = "choices-agent.mjs"
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.New("選択肢のコード解析に失敗しました")
	}
	var choices []any
	if err := json.Unmarshal(output, &choices); err != nil {
		return nil, err
	}
	return choices, nil
}
