package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryDoesNotOfferBackups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"settings.json", ".config-sup-backup-old"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`{"mode":"auto"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := scanLocal()
	if err != nil || len(files) != 1 || files[0].Path != filepath.Join(root, "settings.json") {
		t.Fatalf("backup offered: %#v %v", files, err)
	}
}

func TestMultipleFilesShareOneSettingsList(t *testing.T) {
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
	finder := func(local []localFile, _ string) ([]int, error) {
		ids := []int{}
		for _, path := range []string{jsonPath, tomlPath} {
			found := false
			for _, file := range local {
				if file.Path == path {
					ids = append(ids, file.ID)
					found = true
				}
			}
			if !found {
				t.Fatalf("missing file %s", path)
			}
		}
		return ids, nil
	}
	var out bytes.Buffer
	if err := runCLI("example", strings.NewReader("y\n1,2\n2\nN\nmanual\nN\n"), &out, finder); err != nil {
		t.Fatal(err)
	}
	listing := strings.SplitN(strings.SplitN(out.String(), "設定項目  現在の内容\n", 2)[1], "変更する項目の番号", 2)[0]
	if !strings.Contains(listing, "enabled = true") || !strings.Contains(listing, `mode = "auto"`) || strings.Contains(listing, jsonPath) || strings.Contains(listing, tomlPath) {
		t.Fatalf("listing: %s", listing)
	}
	if content, _ := os.ReadFile(tomlPath); string(content) != "mode = \"auto\"\n" {
		t.Fatal("wrote without approval")
	}
	out.Reset()
	if err := runCLI("example", strings.NewReader("y\n1,2\n2\nN\nmanual\ny\n"), &out, finder); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(tomlPath); string(content) != "mode = \"manual\"\n" {
		t.Fatalf("wrong target: %s", content)
	}
	if content, _ := os.ReadFile(jsonPath); string(content) != `{"enabled":true}` {
		t.Fatalf("changed unrelated file: %s", content)
	}
}

func TestMultipleFileSelectionRejectsDuplicates(t *testing.T) {
	var out bytes.Buffer
	local := []localFile{{Path: "one"}, {Path: "two"}}
	for _, answer := range []string{"", "0", "1,1", "1,3", "all"} {
		if _, err := chooseFiles(bufio.NewReader(strings.NewReader(answer+"\n")), &out, local, []int{0, 1}); err == nil {
			t.Fatalf("accepted %q", answer)
		}
		out.Reset()
	}
}

func TestDuplicateSettingNamesCannotSilentlyPickOneFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "settings.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`{"mode":"auto"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	err := runCLI("example", strings.NewReader("y\n1,2\n1\n"), &out, func(local []localFile, _ string) ([]int, error) {
		ids := []int{}
		for _, file := range local {
			if filepath.Dir(file.Path) == root {
				ids = append(ids, file.ID)
			}
		}
		return ids, nil
	})
	if err == nil || !strings.Contains(err.Error(), "同名項目") || !strings.Contains(out.String(), "(重複)") {
		t.Fatalf("ambiguity not blocked: %v / %s", err, out.String())
	}
	for _, name := range []string{"config.json", "settings.json"} {
		if data, _ := os.ReadFile(filepath.Join(root, name)); string(data) != `{"mode":"auto"}` {
			t.Fatalf("ambiguous file changed: %s", data)
		}
	}
}

func TestCLIOnlyAppliesAfterHumanY(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	original := `{"enabled":true,"privateToken":"untouched"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	finder := func(local []localFile, request string) ([]int, error) {
		if request != "example tool" || len(local) != 1 {
			t.Fatalf("unexpected discovery: %s %#v", request, local)
		}
		return []int{local[0].ID}, nil
	}
	var out bytes.Buffer
	if err := runCLI("example tool", strings.NewReader("y\n1\nfalse\nN\n"), &out, finder); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("wrote without approval")
	}
	out.Reset()
	if err := runCLI("example tool", strings.NewReader("y\n1\nfalse\ny\n"), &out, finder); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `"enabled": false`) || !strings.Contains(out.String(), `enabled: true → false`) {
		t.Fatalf("unexpected result: %s / %s", data, out.String())
	}
}
func TestTUIPicksOnlyListedSetting(t *testing.T) {
	bin := t.TempDir()
	path := filepath.Join(bin, "fzf")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsed -n '2p'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	index, err := pickTUI([]localValue{{Path: []string{"first"}, Value: "a"}, {Path: []string{"second"}, Value: "b"}}, nil)
	if err != nil || index != 1 {
		t.Fatalf("pick: %d %v", index, err)
	}
	if displayPath([]string{"line\ninjected"}) != `line\ninjected` || displayPath([]string{"\x1b[31m"}) != `\x1b[31m` {
		t.Fatal("control character not escaped")
	}
}

func TestCLICodeReadingRequiresSeparateConsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "app")
	os.MkdirAll(root, 0700)
	path := filepath.Join(root, "settings.json")
	os.WriteFile(path, []byte(`{"mode":"auto"}`), 0600)
	var out bytes.Buffer
	err := runCLI("app", strings.NewReader("y\n1\nN\nmanual\nN\n"), &out,
		func([]localFile, string) ([]int, error) { return []int{0}, nil })
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"mode":"auto"}` || !strings.Contains(out.String(), "コード内容をPiモデルへ送信") {
		t.Fatalf("unexpected: %s / %s", data, out.String())
	}
}

func TestCLIExtraSearchRequiresSeparateConsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config")
	os.MkdirAll(root, 0700)
	os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"enabled":true}`), 0600)
	var out bytes.Buffer
	err := runWorkflow("missing", strings.NewReader("y\nN\n"), &out,
		func([]localFile, string) ([]int, error) { return nil, nil }, false,
		func([]localFile, string) ([]int, error) { t.Fatal("extra search without consent"); return nil, nil })
	if err == nil || !strings.Contains(err.Error(), "追加探索せず") {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestCLIAsksForToolWhenNoArgument(t *testing.T) {
	var out bytes.Buffer
	err := runCLI("", strings.NewReader("Zed エディタ\nN\n"), &out,
		func([]localFile, string) ([]int, error) { t.Fatal("searched without consent"); return nil, nil })
	if err == nil || !strings.Contains(out.String(), "tool: ") {
		t.Fatalf("unexpected prompt: %v / %s", err, out.String())
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

func TestCLINoSearchWithoutConsent(t *testing.T) {
	var out bytes.Buffer
	err := runCLI("example", strings.NewReader("N\n"), &out, func([]localFile, string) ([]int, error) { t.Fatal("finder called"); return nil, nil })
	if err == nil {
		t.Fatal("accepted without consent")
	}
}
