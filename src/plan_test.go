package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPlanPreviewAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"mode":"auto","enabled":true,"apiToken":"never send","big":9007199254740993}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	local, err := scanLocal()
	if err != nil || len(local) != 1 {
		t.Fatalf("local: %#v %v", local, err)
	}
	metadata, _ := json.Marshal(local)
	if strings.Contains(string(metadata), "never send") || strings.Contains(string(metadata), "apiToken") {
		t.Fatal("private data in agent metadata")
	}
	files := []source{{File: "config.json", Fields: []field{{Path: []string{"mode"}, Type: "string", Value: "default", Choices: []any{"auto", "manual"}, Target: &target{ID: 0, Path: []string{"mode"}}}}}}
	_, plan, err := preparePlan("https://github.com/example/repo", files, nil, local)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Fields[0].Value != "auto" || files[0].Fields[0].Target.File != path {
		t.Fatalf("mapping: %#v", files)
	}
	if err := plan.apply("not-previewed"); err == nil {
		t.Fatal("applied without preview")
	}
	if _, _, err := plan.preview([]pendingChange{{ID: "0:0", Value: true}}); err == nil {
		t.Fatal("accepted wrong value type")
	}
	id, diff, err := plan.preview([]pendingChange{{ID: "0:0", Value: "manual"}})
	if err != nil || !strings.Contains(diff, path) {
		t.Fatalf("preview: %q %v", diff, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("preview wrote to disk")
	}
	if err := plan.apply(id); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	var result map[string]any
	if json.Unmarshal(data, &result) != nil || result["mode"] != "manual" || result["apiToken"] != "never send" || !strings.Contains(string(data), "9007199254740993") {
		t.Fatalf("applied: %s", data)
	}
	if err := plan.apply(id); err == nil {
		t.Fatal("replayed apply")
	}
}

func TestLocalPlanStopsChangedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config")
	os.MkdirAll(root, 0700)
	path := filepath.Join(root, "settings.json")
	os.WriteFile(path, []byte(`{"enabled":true}`), 0600)
	local, _ := scanLocal()
	files := []source{{File: "settings.json", Fields: []field{{Path: []string{"enabled"}, Type: "boolean", Value: true, Target: &target{ID: 0, Path: []string{"enabled"}}}}}}
	_, plan, err := preparePlan("repo", files, nil, local)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := plan.preview([]pendingChange{{ID: "0:0", Value: false}})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"enabled":true,"other":1}`), 0600)
	if err := plan.apply(id); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
}
