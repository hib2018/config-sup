package main

import "testing"

func TestCommandArgsDefaultsToTUI(t *testing.T) {
	for _, test := range []struct {
		args    []string
		tui     bool
		request string
	}{
		{nil, true, ""},
		{[]string{"Ghostty"}, true, "Ghostty"},
		{[]string{"--cli", "Ghostty"}, false, "Ghostty"},
		{[]string{"--tui", "Ghostty"}, true, "Ghostty"},
	} {
		tui, request := commandArgs(test.args)
		if tui != test.tui || request != test.request {
			t.Fatalf("%v: tui=%t request=%q", test.args, tui, request)
		}
	}
}
