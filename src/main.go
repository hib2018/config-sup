package main

import (
	"fmt"
	"os"
	"strings"
)

func commandArgs(args []string) (bool, string) {
	if len(args) > 0 && (args[0] == "--cli" || args[0] == "--tui") {
		return args[0] == "--tui", strings.Join(args[1:], " ")
	}
	return true, strings.Join(args, " ")
}

func main() {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(os.Stderr, "承認を伴うため、対話端末から実行してください")
		os.Exit(1)
	}
	tui, request := commandArgs(os.Args[1:])
	workflow := runCLI
	if tui {
		workflow = runTUI
	}
	if err := workflow(request, os.Stdin, os.Stdout, findTool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
