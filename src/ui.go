package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
)

type settingEntry struct {
	id    int
	value localValue
	data  []byte
}

func settingLabel(index int, value localValue, info settingInfo, duplicated bool, draft any) string {
	label := info.Description
	if label == "" {
		label = "説明を取得できません（編集不可）"
	}
	if duplicated {
		label += "（重複・編集不可）"
	}
	text := fmt.Sprintf("%d. %s — 現在: %s", index+1, label, printable(value.Value))
	if draft != nil {
		text += fmt.Sprintf(" → %s", printable(draft))
	}
	if value.Type == "boolean" {
		return text + " | 選択肢: 有効 / 無効"
	}
	if len(info.Choices) >= 2 {
		choices := make([]string, len(info.Choices))
		for index, choice := range info.Choices {
			choices[index] = printable(choice)
			if len(info.ChoiceLabels) == len(info.Choices) && info.ChoiceLabels[index] != "" {
				choices[index] += "（" + info.ChoiceLabels[index] + "）"
			}
		}
		return text + " | 選択肢: " + strings.Join(choices, " / ")
	}
	return text + " | 自由入力"
}

// A number followed by Space selects an item. Enter without a number finishes.
func pickTUI(rows []string) (int, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return 0, errors.New("TUIにはfzfが必要です。番号式CLIは --cli で起動できます")
	}
	cmd := exec.Command("fzf", "--prompt", "番号 + Space> ", "--height", "~60%", "--layout", "reverse", "--no-sort", "--print-query", "--bind", "space:accept,enter:abort")
	cmd.Stdin = strings.NewReader(strings.Join(rows, "\n") + "\n")
	cmd.Stderr = os.Stderr
	result, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && (exit.ExitCode() == 1 || exit.ExitCode() == 130) {
			return -1, nil
		}
		return 0, fmt.Errorf("fzfを起動できません: %w", err)
	}
	query, _, _ := strings.Cut(string(result), "\n")
	number, err := strconv.Atoi(strings.TrimSpace(query))
	if err != nil || number < 1 || number > len(rows) {
		return 0, errors.New("番号を入力してSpaceで選択してください")
	}
	return number - 1, nil
}
func selectSetting(reader *bufio.Reader, out io.Writer, rows []string, tui bool) (int, error) {
	if tui {
		return pickTUI(rows)
	}
	for _, row := range rows {
		fmt.Fprintln(out, row)
	}
	fmt.Fprint(out, "番号 + Space（CLIではEnter。空欄で確認）: ")
	text, err := answer(reader)
	if err != nil {
		return 0, err
	}
	if text == "" {
		return -1, nil
	}
	number, err := strconv.Atoi(text)
	if err != nil || number < 1 || number > len(rows) {
		return 0, errors.New("項目番号が不正です")
	}
	return number - 1, nil
}
func editSetting(reader *bufio.Reader, out io.Writer, item localValue, info settingInfo, current any) (any, error) {
	if item.Type == "boolean" {
		return !current.(bool), nil
	}
	if len(info.Choices) >= 2 {
		for index, choice := range info.Choices {
			if reflect.DeepEqual(current, choice) {
				return info.Choices[(index+1)%len(info.Choices)], nil
			}
		}
		return info.Choices[0], nil
	}
	fmt.Fprint(out, "新しい値（自由入力）: ")
	text, err := answer(reader)
	if err != nil {
		return nil, err
	}
	switch item.Type {
	case "string":
		return text, nil
	case "number":
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 9007199254740991 {
			return nil, errors.New("有効な数値を入力してください")
		}
		return number, nil
	}
	return nil, errors.New("未対応の値です")
}
