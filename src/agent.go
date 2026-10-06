package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

func inspectRepo(root string) ([]source, map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "node", "src/agent.mjs", root).Output()
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
