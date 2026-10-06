package main

import (
	"strings"
	"testing"
)

func TestAllowedDescriptions(t *testing.T) {
	files := []source{{File: "config.json", Fields: []field{{Path: []string{"a", "b"}, Type: "boolean", Value: true}}}}
	long := strings.Repeat("あ", 301)
	result := allowedDescriptions(files, map[string]string{"config.json|a.b": long, "config.json|unknown": "fake"})
	if len(result) != 1 || len([]rune(result["config.json|a.b"])) != 300 {
		t.Fatalf("unexpected descriptions: %#v", result)
	}
}
