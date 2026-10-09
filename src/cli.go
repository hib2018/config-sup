package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func findTool(local []localFile, request string) ([]int, error) {
	payload, err := json.Marshal(map[string]any{"local": local, "request": request})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	script := "src/local-agent.mjs"
	if _, err := os.Stat(script); err != nil {
		script = "local-agent.mjs"
	} // go test runs inside src/.
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.New("Piエージェントによるツール特定に失敗しました。モデルと認証を確認してください")
	}
	var ids []int
	if err := json.Unmarshal(output, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}
func answer(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", io.EOF
	}
	return strings.TrimSpace(line), nil
}
func yes(reader *bufio.Reader, out io.Writer, question string) (bool, error) {
	fmt.Fprintf(out, "%s [yes/No]: ", question)
	text, err := answer(reader)
	return text == "yes", err
}

func runCLI(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error)) error {
	return runWorkflow(request, in, out, find, false)
}
func runTUI(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error)) error {
	return runWorkflow(request, in, out, find, true)
}
func displayPath(path []string) string {
	return strings.NewReplacer("\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(strings.Join(path, "."))
}
func pickTUI(values []localValue) (int, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return 0, errors.New("TUIにはfzfが必要です。CLIは --tui なしで起動できます")
	}
	rows := make([]string, len(values))
	for index, value := range values {
		rows[index] = fmt.Sprintf("%d. %s = %s", index+1, displayPath(value.Path), printable(value.Value))
	}
	cmd := exec.Command("fzf", "--prompt", "設定項目> ", "--height", "~60%", "--layout", "reverse", "--no-multi")
	cmd.Stdin = strings.NewReader(strings.Join(rows, "\n") + "\n")
	cmd.Stderr = os.Stderr
	selected, err := cmd.Output()
	if err != nil {
		return 0, errors.New("TUIの選択を中止しました")
	}
	for index, row := range rows {
		if strings.TrimSuffix(string(selected), "\n") == row {
			return index, nil
		}
	}
	return 0, errors.New("不正な選択です")
}
func runWorkflow(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error), tui bool) error {
	reader := bufio.NewReader(in)
	if request == "" {
		fmt.Fprint(out, "対象ツールを自然言語で指定: ")
		var err error
		request, err = answer(reader)
		if err != nil {
			return err
		}
	}
	if request == "" {
		return errors.New("ツールを指定してください")
	}
	ok, err := yes(reader, out, "~/.config と ~/Library/Application Support を検索し、ファイル名・キー名・型（値は含まない）をPiモデルへ送信してよいですか？")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("解析は取り消されました")
	}
	local, err := scanLocal()
	if err != nil {
		return err
	}
	ids, err := find(local, request)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("対象が見つかりませんでした。追加ディレクトリの承認付き探索はまだ未実装です。探索範囲は広げていません")
	}
	id := ids[0]
	if len(ids) > 1 {
		fmt.Fprintln(out, "対象を特定できません。候補:")
		for index, candidate := range ids {
			if candidate < 0 || candidate >= len(local) {
				return errors.New("不正な候補です")
			}
			fmt.Fprintf(out, "%d. %s\n", index+1, local[candidate].Path)
		}
		fmt.Fprint(out, "番号（それ以外は中止）: ")
		choice, err := answer(reader)
		if err != nil {
			return err
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 1 || number > len(ids) {
			return errors.New("中止しました")
		}
		id = ids[number-1]
	}
	if id < 0 || id >= len(local) {
		return errors.New("不正な候補です")
	}
	candidate := local[id]
	data, err := os.ReadFile(candidate.Path)
	if err != nil {
		return err
	}
	values, err := inspectValues(candidate.Path, data)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return errors.New("表示可能な設定項目がありません")
	}
	index := 0
	if tui {
		index, err = pickTUI(values)
		if err != nil {
			return err
		}
	} else {
		fmt.Fprintln(out, "設定項目  現在の内容")
		for number, value := range values {
			fmt.Fprintf(out, "%d. %s = %s\n", number+1, displayPath(value.Path), printable(value.Value))
		}
		fmt.Fprint(out, "変更する項目の番号（空欄で終了）: ")
		choice, err := answer(reader)
		if err != nil {
			return err
		}
		if choice == "" {
			return nil
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 1 || number > len(values) {
			return errors.New("項目番号が不正です")
		}
		index = number - 1
	}
	item := values[index]
	if item.Type == "boolean" {
		fmt.Fprint(out, "新しい値（true/false）: ")
	} else if item.Type == "number" {
		fmt.Fprint(out, "新しい数値: ")
	} else {
		fmt.Fprint(out, "新しい文字列: ")
	}
	text, err := answer(reader)
	if err != nil {
		return err
	}
	var next any
	switch item.Type {
	case "boolean":
		if text != "true" && text != "false" {
			return errors.New("true または false を入力してください")
		}
		next = text == "true"
	case "number":
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 9007199254740991 {
			return errors.New("有効な数値を入力してください")
		}
		next = number
	case "string":
		next = text
	default:
		return errors.New("未対応の値です")
	}
	files := []source{{File: candidate.Path, Fields: []field{{Path: item.Path, Type: item.Type, Value: item.Value, Target: &target{ID: candidate.ID, Path: item.Path}}}}}
	_, plan, err := preparePlan("local", files, nil, local)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, plan.Original[candidate.Path]) || !reflect.DeepEqual(item.Value, files[0].Fields[0].Value) {
		return errors.New("一覧表示後に設定ファイルが変わりました。再解析してください")
	}
	previewID, _, err := plan.preview([]pendingChange{{ID: "0:0", Value: next}})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %s → %s\n", displayPath(item.Path), printable(item.Value), printable(next))
	ok, err = yes(reader, out, "この設定内容を適用しますか？")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "適用せず終了しました")
		return nil
	}
	applyErr := plan.apply(previewID)
	for path, backup := range plan.Backups {
		fmt.Fprintf(out, "バックアップ: %s （元: %s）\n", backup, path)
	}
	if applyErr != nil {
		return applyErr
	}
	fmt.Fprintln(out, "適用しました")
	return nil
}
