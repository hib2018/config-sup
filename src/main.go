package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(os.Stderr, "承認を伴うため、対話端末から実行してください")
		os.Exit(1)
	}
	args := os.Args[1:]
	tui := len(args) > 0 && args[0] == "--tui"
	if tui {
		args = args[1:]
	}
	workflow := runCLI
	if tui {
		workflow = runTUI
	}
	if err := workflow(strings.Join(args, " "), os.Stdin, os.Stdout, findTool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
