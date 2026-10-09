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
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func findTool(local []localFile, request string) ([]int, error) {
	return queryAgent(local, request, "config")
}
func findApp(local []localFile, request string) ([]int, error) {
	return queryAgent(local, request, "app")
}
func queryAgent(local []localFile, request, mode string) ([]int, error) {
	payload, err := json.Marshal(map[string]any{"local": local, "request": request, "mode": mode})
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
	return runWorkflow(request, in, out, find, false, findApp)
}
func runTUI(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error)) error {
	return runWorkflow(request, in, out, find, true, findApp)
}
func displayPath(path []string) string {
	quoted := strconv.Quote(strings.Join(path, "."))
	return quoted[1 : len(quoted)-1]
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
func runWorkflow(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error), tui bool, appFinder func([]localFile, string) ([]int, error)) error {
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
	if err != nil && !errors.Is(err, errNoLocal) {
		return err
	}
	ids := []int{}
	appPath := ""
	if len(local) > 0 {
		ids, err = find(local, request)
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		approved, err := yes(reader, out, "初期範囲では対象を特定できませんでした。追加で /Applications と ~/Applications のアプリ名だけを読み取り専用で探し、Piモデルへ送ってよいですか？")
		if err != nil {
			return err
		}
		if !approved {
			return errors.New("追加探索せず終了しました")
		}
		apps := scanApps()
		if len(apps) == 0 {
			return errors.New("追加範囲にアプリが見つかりませんでした")
		}
		appIDs, err := appFinder(apps, request)
		if err != nil {
			return err
		}
		if len(appIDs) == 0 {
			return errors.New("追加範囲でも対象ツールを特定できませんでした")
		}
		if len(appIDs) > 1 {
			fmt.Fprintln(out, "アプリ候補が複数あります:")
			for index, candidate := range appIDs {
				if candidate < 0 || candidate >= len(apps) {
					return errors.New("不正なアプリ候補です")
				}
				fmt.Fprintf(out, "%d. %q\n", index+1, apps[candidate].Path)
			}
			fmt.Fprint(out, "番号（それ以外は中止）: ")
			choice, err := answer(reader)
			if err != nil {
				return err
			}
			number, err := strconv.Atoi(choice)
			if err != nil || number < 1 || number > len(appIDs) {
				return errors.New("中止しました")
			}
			appIDs = []int{appIDs[number-1]}
		}
		appID := appIDs[0]
		if appID < 0 || appID >= len(apps) {
			return errors.New("不正なアプリ候補です")
		}
		appPath = apps[appID].Path
		appName := strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath))
		fmt.Fprintf(out, "アプリ本体を確認: %q （設定の書き込み先にはしません）\n", appPath)
		if len(local) > 0 {
			ids, err = find(local, appName)
			if err != nil {
				return err
			}
		}
		if len(ids) == 0 {
			return errors.New("アプリ本体は見つかりましたが、初期範囲に対応する編集可能な設定ファイルが見つかりませんでした。アプリ本体は変更しません")
		}
	}
	id := ids[0]
	if len(ids) > 1 {
		fmt.Fprintln(out, "対象を特定できません。候補:")
		for index, candidate := range ids {
			if candidate < 0 || candidate >= len(local) {
				return errors.New("不正な候補です")
			}
			fmt.Fprintf(out, "%d. %q\n", index+1, local[candidate].Path)
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
	var choices []any
	if item.Type != "boolean" {
		roots := []string{filepath.Dir(candidate.Path)}
		if appPath != "" {
			roots = append(roots, appPath)
		}
		approved, err := yes(reader, out, "選択肢を調べるため"+fmt.Sprintf("%q", roots)+"内のソースコード名と必要なコード内容をPiモデルへ送信してよいですか？")
		if err != nil {
			return err
		}
		if approved {
			paths := scanCodePaths(roots)
			if len(paths) > 0 {
				choices, err = findChoices(paths, item, request)
				if err != nil {
					fmt.Fprintf(out, "選択肢の解析は利用できません: %v\n", err)
				}
			}
		}
	}
	var next any
	if len(choices) >= 2 {
		fmt.Fprintln(out, "コードに根拠のある選択肢（推測を含む可能性があるため確認してください）:")
		for number, value := range choices {
			fmt.Fprintf(out, "%d. %s\n", number+1, printable(value))
		}
		fmt.Fprint(out, "番号で選択（0で自由入力）: ")
		choice, err := answer(reader)
		if err != nil {
			return err
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 0 || number > len(choices) {
			return errors.New("選択番号が不正です")
		}
		if number > 0 {
			next = choices[number-1]
		}
	}
	if next == nil && item.Type == "boolean" {
		fmt.Fprint(out, "新しい値（true/false）: ")
	} else if next == nil && item.Type == "number" {
		fmt.Fprint(out, "新しい数値: ")
	} else if next == nil {
		fmt.Fprint(out, "新しい文字列: ")
	}
	if next == nil {
		text, err := answer(reader)
		if err != nil {
			return err
		}
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
		fmt.Fprintf(out, "バックアップ: %q （元: %q）\n", backup, path)
	}
	if applyErr != nil {
		return applyErr
	}
	fmt.Fprintln(out, "適用しました")
	return nil
}
