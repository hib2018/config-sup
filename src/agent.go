package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

func describe(files []source) (map[string]string, string) {
	empty := map[string]string{}
	payload, err := json.Marshal(files)
	if err != nil {
		return empty, "エージェント入力の作成に失敗しました"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "src/agent.mjs")
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		return empty, "Pi SDK の処理に失敗しました（Pi のモデル・認証設定を確認してください）"
	}
	var result struct {
		Descriptions map[string]string `json:"descriptions"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Descriptions == nil {
		return empty, "Pi SDK の応答形式が不正です"
	}
	return allowedDescriptions(files, result.Descriptions), "AIによる説明は参考情報です"
}

func allowedDescriptions(files []source, descriptions map[string]string) map[string]string {
	allowed := map[string]bool{}
	for _, file := range files {
		for _, field := range file.Fields {
			allowed[file.File+"|"+strings.Join(field.Path, ".")] = true
		}
	}
	result := map[string]string{}
	for key, description := range descriptions {
		if allowed[key] {
			chars := []rune(description)
			result[key] = string(chars[:min(len(chars), 300)])
		}
	}
	return result
}
