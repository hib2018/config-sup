package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIOnlyAppliesAfterHumanYes(t *testing.T) {
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
	if err := runCLI("example tool", strings.NewReader("yes\n1\nfalse\nno\n"), &out, finder); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("wrote without approval")
	}
	out.Reset()
	if err := runCLI("example tool", strings.NewReader("yes\n1\nfalse\nyes\n"), &out, finder); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `"enabled": false`) || !strings.Contains(out.String(), `enabled: true → false`) {
		t.Fatalf("unexpected result: %s / %s", data, out.String())
	}
}
func TestCLINoSearchWithoutConsent(t *testing.T) {
	var out bytes.Buffer
	err := runCLI("example", strings.NewReader("no\n"), &out, func([]localFile, string) ([]int, error) { t.Fatal("finder called"); return nil, nil })
	if err == nil {
		t.Fatal("accepted without consent")
	}
}
