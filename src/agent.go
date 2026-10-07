package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

func inspectRepo(root string, local []localFile) ([]source, map[string]string, error) {
	payload, err := json.Marshal(map[string]any{"local": local})
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "src/agent.mjs", root)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, errors.New("Pi の解析が120秒以内に終わりませんでした")
		}
		return nil, nil, errors.New("Pi SDK の解析に失敗しました。Pi のモデル・認証設定、または対象リポジトリを確認してください")
	}
	var result struct {
		Files        []source          `json:"files"`
		Descriptions map[string]string `json:"descriptions"`
	}
	if json.Unmarshal(output, &result) != nil || len(result.Files) == 0 {
		return nil, nil, errors.New("Pi SDK の応答形式が不正です")
	}
	return result.Files, result.Descriptions, nil
}
