package main

import (
	"net/http"
	"net/http/httptest"
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

func TestAnalyzeRequiresConsent(t *testing.T) {
	if _, err := analyze("https://github.com/example/project", false); err == nil {
		t.Fatal("accepted analysis without consent")
	}
}

func TestAnalyzeRequiresLocalOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(`{"url":"https://github.com/a/b","useAgent":true}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler("4173").ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", res.Code)
	}
}
