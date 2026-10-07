package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type field struct {
	Path    []string `json:"path"`
	Type    string   `json:"type"`
	Value   any      `json:"value"`
	Choices []any    `json:"choices,omitempty"`
	Target  *target  `json:"target,omitempty"`
}
type target struct {
	ID   int      `json:"id"`
	Path []string `json:"path"`
	File string   `json:"file,omitempty"`
}
type source struct {
	File   string  `json:"file"`
	Fields []field `json:"fields"`
}

var repoPart = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func githubRepo(input string) (string, error) {
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return "", errors.New("公開GitHubリポジトリのURL（https://github.com/owner/repo）を指定してください")
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." {
		return "", errors.New("公開GitHubリポジトリのURL（https://github.com/owner/repo）を指定してください")
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if repo == "" || repo == "." || repo == ".." {
		return "", errors.New("リポジトリ名を指定してください")
	}
	return "https://github.com/" + parts[0] + "/" + repo, nil
}

func analyze(input string, useAgent bool) (any, *plan, error) {
	if !useAgent {
		return nil, nil, errors.New("解析にはPiのモデルが必要です。送信内容を確認し、画面で同意してください")
	}
	repo, err := githubRepo(input)
	if err != nil {
		return nil, nil, err
	}
	local, err := scanLocal()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "config-sup-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "repo")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "protocol.file.allow=never", "clone", "--quiet", "--depth=1", "--no-checkout", "--filter=blob:none", repo, dest)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LFS_SKIP_SMUDGE=1")
	if _, err := cmd.Output(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("GitHub からの取得が30秒以内に終わりませんでした: %w", ctx.Err())
		}
		return nil, nil, fmt.Errorf("git の取得に失敗しました: %w", err)
	}
	files, descriptions, err := inspectRepo(dest, local)
	if err != nil {
		return nil, nil, err
	}
	return preparePlan(repo, files, descriptions, local)
}
