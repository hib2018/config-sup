package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDescriptions(values []localValue, _ []string, _ string) (map[int]settingInfo, error) {
	result := map[int]settingInfo{}
	for i, value := range values {
		label := map[string]string{"enabled": "機能の有効・無効", "mode": "動作モード"}[value.Path[0]]
		if label == "" {
			label = "設定内容"
		}
		result[i] = settingInfo{ID: i, Description: label}
	}
	return result, nil
}
func testCLI(request, input string, out *bytes.Buffer, find func([]localFile, string) ([]int, error)) error {
	return runWorkflow(request, strings.NewReader(input), out, find, false, func([]localFile, string) ([]int, error) { return nil, nil }, testDescriptions)
}
func settingsFixture(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	jsonPath, tomlPath := filepath.Join(root, "config.json"), filepath.Join(root, "settings.toml")
	if err := os.WriteFile(jsonPath, []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tomlPath, []byte("mode = \"auto\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return jsonPath, tomlPath
}
func fixtureFinder(paths ...string) func([]localFile, string) ([]int, error) {
	return func(local []localFile, _ string) ([]int, error) {
		ids := []int{}
		for _, path := range paths {
			for _, file := range local {
				if file.Path == path {
					ids = append(ids, file.ID)
				}
			}
		}
		return ids, nil
	}
}
func TestUnifiedListAndOnlyFinalApproval(t *testing.T) {
	jsonPath, tomlPath := settingsFixture(t)
	var out bytes.Buffer
	finder := fixtureFinder(jsonPath, tomlPath)
	if err := testCLI("example", "1,2\n2 \nmanual\n\nN\n", &out, finder); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "機能の有効・無効") || !strings.Contains(text, "動作モード") || strings.Contains(text, "mode =") || strings.Count(text, "[y/N]") != 1 {
		t.Fatalf("unexpected UI: %s", text)
	}
	if content, _ := os.ReadFile(tomlPath); string(content) != "mode = \"auto\"\n" {
		t.Fatal("wrote before approval")
	}
	out.Reset()
	if err := testCLI("example", "1,2\n2 \nmanual\n\ny\n", &out, finder); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(tomlPath); string(content) != "mode = \"manual\"\n" {
		t.Fatalf("wrong target: %q", content)
	}
	if content, _ := os.ReadFile(jsonPath); string(content) != `{"enabled":true}` {
		t.Fatalf("changed unrelated file: %q", content)
	}
}
func TestToggleAndBatchApplyAcrossFiles(t *testing.T) {
	jsonPath, tomlPath := settingsFixture(t)
	var out bytes.Buffer
	finder := fixtureFinder(jsonPath, tomlPath)
	if err := testCLI("example", "1,2\n1 \n2 \nmanual\n\ny\n", &out, finder); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(jsonPath); !strings.Contains(string(content), `"enabled": false`) {
		t.Fatalf("toggle failed: %q", content)
	}
	if content, _ := os.ReadFile(tomlPath); string(content) != "mode = \"manual\"\n" {
		t.Fatalf("text edit failed: %q", content)
	}
	if !strings.Contains(out.String(), "動作モード: \"auto\" → \"manual\"") {
		t.Fatalf("missing review: %s", out.String())
	}
}
func TestDuplicateSettingNamesCannotBeEdited(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "config.json"), filepath.Join(root, "settings.json")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(`{"mode":"auto"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	err := testCLI("example", "1,2\n1 \n", &out, fixtureFinder(paths...))
	if err == nil || !strings.Contains(err.Error(), "同名項目") || !strings.Contains(out.String(), "重複") {
		t.Fatalf("ambiguity: %v / %s", err, out.String())
	}
	for _, path := range paths {
		if data, _ := os.ReadFile(path); string(data) != `{"mode":"auto"}` {
			t.Fatal("edited duplicate")
		}
	}
}
func TestDiscoveryExcludesSecretsAndBackups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"settings.json": `{"mode":"auto"}`, ".env": "DB_PASSWORD=abc", "api_key.json": `{"mode":"auto"}`, "credentials.json": `{"mode":"auto"}`, "mixed.json": `{"mode":"auto","apiToken":"abc"}`, ".config-sup-backup-old": `{"mode":"auto"}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := scanLocal()
	if err != nil || len(files) != 1 || files[0].Path != filepath.Join(root, "settings.json") {
		t.Fatalf("secrets offered: %#v %v", files, err)
	}
}
func TestNumberSpaceCyclesVerifiedChoices(t *testing.T) {
	_, path := settingsFixture(t)
	var out bytes.Buffer
	describe := func(values []localValue, _ []string, _ string) (map[int]settingInfo, error) {
		return map[int]settingInfo{0: {ID: 0, Description: "動作モード", Choices: []any{"auto", "manual"}}}, nil
	}
	err := runWorkflow("example", strings.NewReader("1 \n\ny\n"), &out, fixtureFinder(path), false, nil, describe)
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(path); string(content) != "mode = \"manual\"\n" {
		t.Fatalf("option cycle failed: %q", content)
	}
}
func TestTUIUsesNumberThenSpace(t *testing.T) {
	bin := t.TempDir()
	path := filepath.Join(bin, "fzf")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '2\\nselected\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	index, err := pickTUI([]string{"1. 一番目", "2. 二番目"})
	if err != nil || index != 1 {
		t.Fatalf("selection: %d %v", index, err)
	}
}
func TestFileSelectionRejectsDuplicates(t *testing.T) {
	var out bytes.Buffer
	local := []localFile{{Path: "one"}, {Path: "two"}}
	for _, input := range []string{"", "0", "1,1", "1,3", "all"} {
		if _, err := chooseFiles(bufio.NewReader(strings.NewReader(input+"\n")), &out, local, []int{0, 1}); err == nil {
			t.Fatalf("accepted %q", input)
		}
		out.Reset()
	}
}
func TestConfirmationDefaultsToNo(t *testing.T) {
	for _, example := range []struct {
		input string
		want  bool
	}{{"y\n", true}, {"Y\n", true}, {"N\n", false}, {"\n", false}, {"yes\n", false}} {
		var out bytes.Buffer
		got, err := yes(bufio.NewReader(strings.NewReader(example.input)), &out, "確認")
		if err != nil || got != example.want || !strings.Contains(out.String(), "[y/N]") {
			t.Fatalf("input %q: approved=%t err=%v prompt=%s", example.input, got, err, out.String())
		}
	}
}
