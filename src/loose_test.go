package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainSettingsPreserveCommentsAndRefuseAmbiguity(t *testing.T) {
	before := []byte("# title\r\nfont-size = 14 # keep\r\nbackground = #101010\r\n")
	fields := inspectPlain(before)
	if len(fields) != 2 || fields[0].Value != "14" || fields[1].Value != "#101010" {
		t.Fatalf("fields: %#v", fields)
	}
	after, err := patchPlain(before, []formatChange{{Path: []string{"font-size"}, Value: "16"}})
	if err != nil || !bytes.Equal(after, []byte("# title\r\nfont-size = 16 # keep\r\nbackground = #101010\r\n")) {
		t.Fatalf("patch: %q %v", after, err)
	}
	if _, err := patchPlain(before, []formatChange{{Path: []string{"font-size"}, Value: "16\nunsafe=true"}}); err == nil {
		t.Fatal("allowed newline injection")
	}
	if fields := inspectPlain([]byte("mode = one\nmode = two\n")); len(fields) != 0 {
		t.Fatalf("accepted duplicates: %#v", fields)
	}
}

func TestLinkedTargetChangePreventsApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "example")
	outside := filepath.Join(home, "dev", "dotfiles")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	first, second := filepath.Join(outside, "first"), filepath.Join(outside, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("mode = auto\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "config")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(alias)
	if err != nil {
		t.Fatal(err)
	}
	local := []localFile{{ID: 0, Path: resolved, Fields: []localField{{Path: []string{"mode"}, Type: "string"}}, Approved: true, LinkAlias: alias}}
	files := []source{{Fields: []field{{Path: []string{"mode"}, Type: "string", Target: &target{ID: 0, Path: []string{"mode"}}}}}}
	plan, err := preparePlan(files, local)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := plan.preview([]pendingChange{{ID: "0:0", Value: "manual"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if err := plan.apply(id); err == nil {
		t.Fatal("applied after link target changed")
	}
	for _, path := range []string{first, second} {
		if content, _ := os.ReadFile(path); string(content) != "mode = auto\n" {
			t.Fatalf("unexpected edit to %s: %s", path, content)
		}
	}
}

func TestPlainFileWithoutKnownExtensionIsSelectedByAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Library", "Application Support", "example")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "custom.settings")
	if err := os.WriteFile(path, []byte("mode = auto\n"), 0600); err != nil {
		t.Fatal(err)
	}
	finder := func(local []localFile, _ string) ([]int, error) {
		for _, candidate := range local {
			if candidate.Path == path {
				if len(candidate.Fields) != 0 {
					t.Fatal("read unknown content before selection")
				}
				return []int{candidate.ID}, nil
			}
		}
		t.Fatal("missing format-independent candidate")
		return nil, nil
	}
	var out bytes.Buffer
	if err := testCLI("example", "1 \nmanual\n\ny\n", &out, finder); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != "mode = manual\n" {
		t.Fatalf("unexpected file: %q", after)
	}
}

func TestLinkedUnspecifiedFormatRequiresOnlyFinalApproval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := filepath.Join(home, "dev", "dotfiles", "ghostty")
	inside := filepath.Join(home, ".config", "ghostty")
	for _, dir := range []string{outside, inside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(outside, "config")
	before := []byte("font-size = 14 # keep\n")
	if err := os.WriteFile(target, before, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(inside, "config")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	finder := func(local []localFile, _ string) ([]int, error) {
		for _, file := range local {
			if file.Path == alias {
				if !file.Link || len(file.Fields) != 0 {
					t.Fatalf("read link before approval: %#v", file)
				}
				encoded, _ := json.Marshal(file)
				if strings.Contains(string(encoded), "font-size") {
					t.Fatal("target content in model metadata")
				}
				return []int{file.ID}, nil
			}
		}
		t.Fatal("missing link candidate")
		return nil, nil
	}
	var out bytes.Buffer
	if err := testCLI("Ghostty", "1 \n16\n\nN\n", &out, finder); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "[y/N]") != 1 {
		t.Fatalf("unexpected approval prompts: %s", out.String())
	}
	if content, _ := os.ReadFile(target); !bytes.Equal(content, before) {
		t.Fatal("wrote before final approval")
	}
	out.Reset()
	if err := testCLI("Ghostty", "1 \n16\n\ny\n", &out, finder); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(target); string(content) != "font-size = 16 # keep\n" {
		t.Fatalf("unexpected edit: %s", content)
	}
	if info, _ := os.Lstat(alias); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("replaced symlink")
	}
	backups, err := os.ReadDir(filepath.Join(home, ".local", "state", "config-sup", "backups"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup missing from private state directory: %v %#v", err, backups)
	}
	backup, err := os.ReadFile(filepath.Join(home, ".local", "state", "config-sup", "backups", backups[0].Name()))
	if err != nil || !bytes.Equal(backup, before) {
		t.Fatalf("backup content: %q %v", backup, err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 1 {
		t.Fatalf("backup leaked into dotfiles: %#v", entries)
	}
}
