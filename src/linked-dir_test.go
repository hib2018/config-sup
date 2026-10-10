package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovedDirectoryLinkRejectsRetargeting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	first, second := filepath.Join(home, "first"), filepath.Join(home, "second")
	for _, root := range []string{first, second} {
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(first, "init.lua")
	if err := os.WriteFile(file, []byte("mode = auto\n"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(home, "nvim")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(first)
	actual, _ := filepath.EvalSymlinks(file)
	if err := approvedLinkStillPointsTo(alias, root, actual); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if err := approvedLinkStillPointsTo(alias, root, actual); err == nil {
		t.Fatal("accepted changed directory link")
	}
}

func TestLinkedDirectoryCanSelectMultipleFilesForOneList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	alias := filepath.Join(home, ".config", "nvim")
	root := filepath.Join(home, "dev", "dotfiles", "nvim")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"init.lua": "mode = auto\n", "options.lua": "number = true\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	calls := 0
	find := func(local []localFile, _ string) ([]int, error) {
		calls++
		if calls == 1 {
			for _, file := range local {
				if file.Path == alias {
					return []int{file.ID}, nil
				}
			}
			t.Fatal("missing alias")
		}
		ids := []int{}
		for _, name := range []string{"init.lua", "options.lua"} {
			for _, file := range local {
				if filepath.Base(file.Path) == name {
					ids = append(ids, file.ID)
				}
			}
		}
		return ids, nil
	}
	var out bytes.Buffer
	if err := runCLI("neovim", strings.NewReader("y\ny\n1,2\ny\ny\n2\nN\nfalse\nN\n"), &out, find); err != nil {
		t.Fatal(err)
	}
	listing := strings.SplitN(strings.SplitN(out.String(), "設定項目  現在の内容\n", 2)[1], "変更する項目の番号", 2)[0]
	if !strings.Contains(listing, `mode = "auto"`) || !strings.Contains(listing, `number = "true"`) || strings.Contains(listing, "init.lua") {
		t.Fatalf("listing: %s", listing)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "options.lua")); string(after) != "number = true\n" {
		t.Fatal("applied without approval")
	}
}

func TestLinkedDirectoryNeedsApprovalBeforeFileNamesAndContents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "nvim")
	actual := filepath.Join(home, "dev", "dotfiles", "nvim", ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(actual, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(actual, "init.lua")
	before := []byte("vim.opt.number = true\n")
	if err := os.WriteFile(file, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, root); err != nil {
		t.Fatal(err)
	}
	realFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	find := func(local []localFile, _ string) ([]int, error) {
		calls++
		for _, candidate := range local {
			if calls == 1 && candidate.Path == root {
				return []int{candidate.ID}, nil
			}
			if calls > 1 && candidate.Path == realFile {
				return []int{candidate.ID}, nil
			}
		}
		t.Fatalf("unexpected file list on call %d: %#v", calls, local)
		return nil, nil
	}
	var out bytes.Buffer
	err = runCLI("neovim", strings.NewReader("y\nN\n"), &out, find)
	if err == nil || !strings.Contains(err.Error(), "リンク先を探索せず") || calls != 1 {
		t.Fatalf("read names without consent: %v calls=%d", err, calls)
	}
	calls = 0
	out.Reset()
	err = runCLI("neovim", strings.NewReader("y\ny\nN\n"), &out, find)
	if err == nil || !strings.Contains(err.Error(), "リンク先を読まず") || calls != 2 {
		t.Fatalf("read file without consent: %v calls=%d", err, calls)
	}
	calls = 0
	out.Reset()
	if err := runCLI("neovim", strings.NewReader("y\ny\ny\n1\nN\nfalse\nN\n"), &out, find); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(file); !bytes.Equal(after, before) {
		t.Fatal("wrote before final approval")
	}
	calls = 0
	out.Reset()
	if err := runCLI("neovim", strings.NewReader("y\ny\ny\n1\nN\nfalse\ny\n"), &out, find); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(file); string(after) != "vim.opt.number = false\n" {
		t.Fatalf("unexpected edit: %q", after)
	}
	if info, _ := os.Lstat(root); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("replaced linked directory")
	}
	if entries, _ := os.ReadDir(actual); len(entries) != 1 {
		t.Fatalf("backup leaked into dotfiles: %#v", entries)
	}
	backups, err := os.ReadDir(filepath.Join(home, ".local", "state", "config-sup", "backups"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("missing external backup: %#v %v", backups, err)
	}
}
