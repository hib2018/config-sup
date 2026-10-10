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

type settingInfo struct {
	ID           int      `json:"id"`
	Description  string   `json:"description"`
	Choices      []any    `json:"choices"`
	ChoiceLabels []string `json:"choiceLabels"`
}

func describeSettings(values []localValue, paths []string, request string) (map[int]settingInfo, error) {
	items := make([]map[string]any, 0, len(values))
	for id, value := range values {
		items = append(items, map[string]any{"id": id, "path": value.Path, "type": value.Type})
	}
	payload, err := json.Marshal(map[string]any{"request": request, "items": items, "paths": paths})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	script := "src/describe-agent.mjs"
	if _, err := os.Stat(script); err != nil {
		script = "describe-agent.mjs"
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.New("項目の説明をPiモデルから取得できませんでした")
	}
	var result []settingInfo
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}
	found := map[int]settingInfo{}
	for _, info := range result {
		if info.ID >= 0 && info.ID < len(values) {
			found[info.ID] = info
		}
	}
	return found, nil
}
