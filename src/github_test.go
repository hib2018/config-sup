package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGithubRepo(t *testing.T) {
	got, err := githubRepo("https://github.com/example/project.git")
	if err != nil || got != "https://github.com/example/project" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"https://github.com.evil.test/a/b", "http://github.com/a/b", "https://github.com/a/b/tree/main", "https://github.com/a/.git", "https://github.com/a/b?x=1", "https://github.com/a/%2e%2e"} {
		if _, err := githubRepo(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestFields(t *testing.T) {
	var value any
	json.Unmarshal([]byte(`{"nested":{"enabled":true,"retries":2,"name":"ok","unused":null},"list":[1]}`), &value)
	got := fieldsFromJSON(value)
	want := []field{{[]string{"nested", "enabled"}, "boolean", true}, {[]string{"nested", "name"}, "string", "ok"}, {[]string{"nested", "retries"}, "number", float64(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"enabled":true}`), 0600)
	os.WriteFile(filepath.Join(root, "other.json"), []byte(`{"ignored":true}`), 0600)
	files, err := discover(root)
	if err != nil || len(files) != 1 || files[0].File != "settings.json" {
		t.Fatalf("%#v %v", files, err)
	}
}

func TestAnalyzeRequiresLocalOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(`{"url":"https://github.com/a/b"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler("4173").ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", res.Code)
	}
}
