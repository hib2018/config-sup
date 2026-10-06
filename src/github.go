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

func discover(root string) ([]source, error) {
	files := []source{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if entry.IsDir() {
			if rel != "." && (depth >= 5 || entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor" || entry.Name() == "dist" || entry.Name() == "build" || len(files) >= 30) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= 30 || !entry.Type().IsRegular() || !configName.MatchString(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 100_000 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 100_000 {
			return nil
		}
		var value any
		if json.Unmarshal(data, &value) != nil {
			return nil
		}
		fields := fieldsFromJSON(value)
		if len(fields) > 0 {
			files = append(files, source{filepath.ToSlash(rel), fields})
		}
		return nil
	})
	return files, err
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
	for _, args := range [][]string{
		{"-c", "protocol.file.allow=never", "clone", "--quiet", "--depth=1", "--no-checkout", "--filter=blob:none", repo, dest},
		{"-C", dest, "-c", "core.hooksPath=/dev/null", "checkout", "--quiet", "HEAD", "--", "."},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LFS_SKIP_SMUDGE=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git の取得に失敗しました: %w: %.200s", err, output)
		}
	}
	files, err := discover(dest)
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
