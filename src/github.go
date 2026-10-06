package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type field struct {
	Path  []string `json:"path"`
	Type  string   `json:"type"`
	Value any      `json:"value"`
}
type source struct {
	File   string  `json:"file"`
	Fields []field `json:"fields"`
}

var configName = regexp.MustCompile(`(?i)^(?:config(?:uration)?|settings|.*[.-]config)(?:\.[\w-]+)?\.json$`)
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

func fieldsFromJSON(value any) []field {
	result := []field{}
	var walk func(any, []string)
	walk = func(value any, path []string) {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		// encoding/json maps lose source order; sorting provides stable output.
		keys := make([]string, 0, len(object))
		for k := range object {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if len(result) >= 100 {
				break
			}
			next := append(append([]string{}, path...), k)
			switch v := object[k].(type) {
			case map[string]any:
				walk(v, next)
			case string:
				result = append(result, field{next, "string", v})
			case float64:
				result = append(result, field{next, "number", v})
			case bool:
				result = append(result, field{next, "boolean", v})
			}
		}
	}
	walk(value, nil)
	return result
}

func configPath(path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) > 6 || !configName.MatchString(parts[len(parts)-1]) {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if slices.Contains([]string{".git", "node_modules", "vendor", "dist", "build"}, part) {
			return false
		}
	}
	return true
}

func discoverGit(ctx context.Context, root string) ([]source, error) {
	files := []source{}
	paths, err := gitCommand(ctx, root, "ls-tree", "-r", "-z", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}
	for _, name := range strings.Split(string(paths), "\x00") {
		if len(files) >= 30 {
			break
		}
		if !configPath(name) {
			continue
		}
		data, err := gitCommand(ctx, root, "show", "HEAD:"+name)
		if err != nil {
			return nil, err
		}
		if len(data) > 100_000 {
			continue
		}
		var value any
		if json.Unmarshal(data, &value) != nil {
			continue
		}
		if fields := fieldsFromJSON(value); len(fields) > 0 {
			files = append(files, source{name, fields})
		}
	}
	return files, nil
}

func gitCommand(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LFS_SKIP_SMUDGE=1")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("GitHub からの取得が30秒以内に終わりませんでした: %w", ctx.Err())
		}
		return nil, fmt.Errorf("git の取得に失敗しました: %w", err)
	}
	return output, nil
}

func analyze(input string, useAgent bool) (any, error) {
	repo, err := githubRepo(input)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "config-sup-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "repo")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := gitCommand(ctx, ".", "-c", "protocol.file.allow=never", "clone", "--quiet", "--depth=1", "--no-checkout", "--filter=blob:none", repo, dest); err != nil {
		return nil, err
	}
	files, err := discoverGit(ctx, dest)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("対応するJSON設定ファイルが見つかりませんでした")
	}
	descriptions := map[string]string{}
	agent := "エージェントは使用していません"
	if useAgent {
		descriptions, agent = describe(files)
	}
	return struct {
		Repo         string            `json:"repo"`
		Files        []source          `json:"files"`
		Descriptions map[string]string `json:"descriptions"`
		Agent        string            `json:"agent"`
	}{repo, files, descriptions, agent}, nil
}
