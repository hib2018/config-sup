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
	fmt.Fprintf(out, "%s [y/N]: ", question)
	text, err := answer(reader)
	return strings.EqualFold(text, "y"), err
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
func pickTUI(values []localValue, counts map[string]int) (int, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return 0, errors.New("TUIにはfzfが必要です。番号式CLIは --cli で起動できます")
	}
	rows := make([]string, len(values))
	for index, value := range values {
		suffix := ""
		if counts[fmt.Sprintf("%q", value.Path)] > 1 {
			suffix = " (重複)"
		}
		rows[index] = fmt.Sprintf("%d. %s = %s%s", index+1, displayPath(value.Path), printable(value.Value), suffix)
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
func chooseFiles(reader *bufio.Reader, out io.Writer, local []localFile, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, errors.New("設定ファイルの候補が見つかりませんでした")
	}
	seen := map[int]bool{}
	for _, id := range ids {
		if id < 0 || id >= len(local) || seen[id] {
			return nil, errors.New("不正な候補です")
		}
		seen[id] = true
	}
	if len(ids) == 1 {
		return ids, nil
	}
	fmt.Fprintln(out, "使用する設定ファイルを選択:")
	for index, id := range ids {
		fmt.Fprintf(out, "%d. %q\n", index+1, local[id].Path)
	}
	fmt.Fprint(out, "番号（複数は 1,2 のように指定。空欄は中止）: ")
	choice, err := answer(reader)
	if err != nil {
		return nil, err
	}
	selected := []int{}
	seen = map[int]bool{}
	for _, part := range strings.Split(choice, ",") {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || number < 1 || number > len(ids) || seen[number] {
			return nil, errors.New("選択を中止しました")
		}
		seen[number] = true
		selected = append(selected, ids[number-1])
	}
	return selected, nil
}

type selectedFile struct {
	file   localFile
	data   []byte
	values []localValue
}

func loadSelected(reader *bufio.Reader, out io.Writer, candidate localFile, request string, find func([]localFile, string) ([]int, error)) ([]selectedFile, error) {
	files := []localFile{candidate}
	if candidate.Link {
		alias := candidate.Path
		resolved, err := filepath.EvalSymlinks(alias)
		if err != nil {
			return nil, fmt.Errorf("リンク先を確認できません: %w", err)
		}
		info, err := os.Lstat(resolved)
		if err != nil {
			return nil, err
		}
		root := ""
		if info.IsDir() {
			root = resolved
			if err := safeApprovedDirectory(root); err != nil {
				return nil, err
			}
			approved, err := yes(reader, out, fmt.Sprintf("設定ディレクトリへのリンク %q の参照先 %q 内だけを探索し、ファイル名をPiモデルへ送信してよいですか？ 内容はまだ読みません", alias, root))
			if err != nil {
				return nil, err
			}
			if !approved {
				return nil, errors.New("リンク先を探索せずに終了しました")
			}
			directory, err := scanLinkedDirectory(root)
			if err != nil {
				return nil, err
			}
			if len(directory) == 0 {
				return nil, errors.New("リンク先に100KB以下の設定候補がありません")
			}
			ids, err := find(directory, request)
			if err != nil {
				return nil, err
			}
			ids, err = chooseFiles(reader, out, directory, ids)
			if err != nil {
				return nil, err
			}
			files = nil
			for _, id := range ids {
				files = append(files, directory[id])
			}
		} else {
			files[0].Path = resolved
		}
		for index := range files {
			files[index].Link = false
			files[index].LinkAlias, files[index].LinkRoot, files[index].Approved = alias, root, true
		}
	}
	selected := []selectedFile{}
	for _, file := range files {
		if file.Approved {
			if err := safeApprovedTarget(file.Path); err != nil {
				return nil, err
			}
			approved, err := yes(reader, out, fmt.Sprintf("設定リンク %q 経由のファイル %q を読み、最終確認後にこのファイルだけを書き換えてよいですか？ 内容のモデル送信は別途確認します", file.LinkAlias, file.Path))
			if err != nil {
				return nil, err
			}
			if !approved {
				return nil, errors.New("リンク先を読まずに終了しました")
			}
			if err := approvedLinkStillPointsTo(file.LinkAlias, file.LinkRoot, file.Path); err != nil {
				return nil, err
			}
		} else if err := safeLocalFile(file.Path); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, err
		}
		if len(data) > 100_000 {
			return nil, errors.New("設定ファイルは100KB以下に限ります")
		}
		values, err := inspectValues(file.Path, data)
		if err != nil || len(values) == 0 {
			fmt.Fprintf(out, "候補 %q は安全に編集できる項目がないため除外しました\n", file.Path)
			continue
		}
		file.Fields = nil
		for _, value := range values {
			file.Fields = append(file.Fields, localField{Path: value.Path, Type: value.Type})
		}
		selected = append(selected, selectedFile{file, data, values})
	}
	return selected, nil
}
func runWorkflow(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error), tui bool, appFinder func([]localFile, string) ([]int, error)) error {
	reader := bufio.NewReader(in)
	if request == "" {
		fmt.Fprint(out, "tool: ")
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
	ids, err = chooseFiles(reader, out, local, ids)
	if err != nil {
		return err
	}
	selected := []selectedFile{}
	for _, id := range ids {
		loaded, err := loadSelected(reader, out, local[id], request, find)
		if err != nil {
			return err
		}
		selected = append(selected, loaded...)
	}
	local = nil
	type setting struct {
		id    int
		value localValue
		data  []byte
	}
	settings := []setting{}
	values := []localValue{}
	for _, selection := range selected {
		selection.file.ID = len(local)
		local = append(local, selection.file)
		for _, value := range selection.values {
			settings = append(settings, setting{selection.file.ID, value, selection.data})
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return errors.New("安全に編集できる設定項目がありません")
	}
	counts := map[string]int{}
	for _, value := range values {
		counts[fmt.Sprintf("%q", value.Path)]++
	}
	index := 0
	if tui {
		index, err = pickTUI(values, counts)
		if err != nil {
			return err
		}
	} else {
		fmt.Fprintln(out, "設定項目  現在の内容")
		for number, value := range values {
			suffix := ""
			if counts[fmt.Sprintf("%q", value.Path)] > 1 {
				suffix = " (重複)"
			}
			fmt.Fprintf(out, "%d. %s = %s%s\n", number+1, displayPath(value.Path), printable(value.Value), suffix)
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
	item := settings[index].value
	if counts[fmt.Sprintf("%q", item.Path)] > 1 {
		return errors.New("同名項目が複数の設定ファイルにあるため編集できません。選択ファイルを絞って再実行してください")
	}
	candidate := local[settings[index].id]
	data := settings[index].data
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
	files := []source{{Fields: []field{{Path: item.Path, Type: item.Type, Value: item.Value, Target: &target{ID: candidate.ID, Path: item.Path}}}}}
	plan, err := preparePlan(files, local)
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
