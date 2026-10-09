package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCodeSearchLimitedToExplicitDirectory(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "app")
	os.MkdirAll(inside, 0700)
	good := filepath.Join(inside, "settings.ts")
	os.WriteFile(good, []byte("const modes = ['auto','manual']"), 0600)
	os.WriteFile(filepath.Join(inside, "auth.ts"), []byte("private"), 0600)
	os.WriteFile(filepath.Join(root, "outside.ts"), []byte("outside"), 0600)
	if got := scanCodePaths([]string{inside}); !reflect.DeepEqual(got, []string{good}) {
		t.Fatalf("unexpected paths: %#v", got)
	}
}
