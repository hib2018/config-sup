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

func TestMultipleLinkedFilesCanApplyTogether(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	inside := filepath.Join(home, ".config", "sample")
	outside := filepath.Join(home, "dev", "dotfiles")
	for _, dir := range []string{inside, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	aliases := []string{}
	targets := []string{}
	for _, name := range []string{"first", "second"} {
		target := filepath.Join(outside, name)
		alias := filepath.Join(inside, name)
		if err := os.WriteFile(target, []byte(name+" = auto\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
		aliases = append(aliases, alias)
		targets = append(targets, target)
	}
	var out bytes.Buffer
	if err := testCLI("sample", "1,2\n1 \nmanual\n2 \nmanual\n\ny\n", &out, fixtureFinder(aliases...)); err != nil {
		t.Fatal(err)
	}
	for _, path := range targets {
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), "manual") {
			t.Fatalf("missing link edit: %q", data)
		}
	}
	backups, err := os.ReadDir(filepath.Join(home, ".local", "state", "config-sup", "backups"))
	if err != nil || len(backups) != 2 {
		t.Fatalf("missing backups: %#v %v", backups, err)
	}
}

func TestLinkedDirectoryExcludesSensitiveFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "dev", "dotfiles", "nvim")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"init.lua": "number = true\n", ".env": "KEY=abc\n", "config.lua": "password = hello\n", "keys.json": `{"enabled":true}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := scanLinkedDirectory(resolved)
	if err != nil || len(files) != 1 || files[0].Path != filepath.Join(resolved, "init.lua") {
		t.Fatalf("sensitive path exposed: %#v %v", files, err)
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
	if err := testCLI("neovim", "1,2\n2 \nfalse\n\nN\n", &out, find); err != nil {
		t.Fatal(err)
	}
	listing := out.String()
	if !strings.Contains(listing, "動作モード") || !strings.Contains(listing, "設定内容") || strings.Contains(listing, "init.lua =") {
		t.Fatalf("listing: %s", listing)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "options.lua")); string(after) != "number = true\n" {
		t.Fatal("applied without approval")
	}
}

func TestLinkedDirectoryNeedsOnlyFinalApproval(t *testing.T) {
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
	if err := testCLI("neovim", "1 \nfalse\n\nN\n", &out, find); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || strings.Count(out.String(), "[y/N]") != 1 {
		t.Fatalf("unexpected approval flow: %s", out.String())
	}
	if after, _ := os.ReadFile(file); !bytes.Equal(after, before) {
		t.Fatal("wrote before final approval")
	}
	calls = 0
	out.Reset()
	if err := testCLI("neovim", "1 \nfalse\n\ny\n", &out, find); err != nil {
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
