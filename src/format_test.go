package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalYAMLAndTOMLApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "sample")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "settings.yaml")
	tomlPath := filepath.Join(dir, "settings.toml")
	before := map[string]string{
		yamlPath: "# keep\nmode: auto # note\nsecret: hidden\n",
		tomlPath: "# keep\nmode = \"auto\" # note\nsecret = \"hidden\"\n",
	}
	for path, content := range before {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	local, err := scanLocal()
	if err != nil || len(local) != 2 {
		t.Fatalf("scan: %#v %v", local, err)
	}
	ids := map[string]int{}
	for _, file := range local {
		ids[file.Path] = file.ID
		for _, field := range file.Fields {
			if strings.Join(field.Path, ".") == "secret" {
				t.Fatal("private metadata leaked")
			}
		}
	}
	files := []source{{Fields: []field{
		{Path: []string{"first"}, Type: "string", Value: "auto", Target: &target{ID: ids[yamlPath], Path: []string{"mode"}}},
		{Path: []string{"second"}, Type: "string", Value: "auto", Target: &target{ID: ids[tomlPath], Path: []string{"mode"}}},
	}}}
	plan, err := preparePlan(files, local)
	if err != nil {
		t.Fatal(err)
	}
	id, diff, err := plan.preview([]pendingChange{{ID: "0:0", Value: "manual"}, {ID: "0:1", Value: "manual"}})
	if err != nil || !strings.Contains(diff, yamlPath) || !strings.Contains(diff, tomlPath) {
		t.Fatalf("preview: %s %v", diff, err)
	}
	for path, text := range before {
		data, _ := os.ReadFile(path)
		if string(data) != text {
			t.Fatal("preview wrote to disk")
		}
	}
	if err := plan.apply(id); err != nil {
		t.Fatal(err)
	}
	for path := range before {
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "# keep") || !strings.Contains(string(data), "# note") || !strings.Contains(string(data), "manual") || !strings.Contains(string(data), "secret") {
			t.Fatalf("lost data: %s", data)
		}
	}
}
