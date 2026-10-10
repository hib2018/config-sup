package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
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
	return runWorkflow(request, in, out, find, false, findApp, describeSettings)
}
func runTUI(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error)) error {
	return runWorkflow(request, in, out, find, true, findApp, describeSettings)
}
func displayPath(path []string) string {
	quoted := strconv.Quote(strings.Join(path, "."))
	return quoted[1 : len(quoted)-1]
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
		if sensitiveContent(data) {
			fmt.Fprintf(out, "候補 %q は機密情報を含む可能性があるため除外しました\n", file.Path)
			continue
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
func runWorkflow(request string, in io.Reader, out io.Writer, find func([]localFile, string) ([]int, error), tui bool, appFinder func([]localFile, string) ([]int, error), describe func([]localValue, []string, string) (map[int]settingInfo, error)) error {
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
	settings := []settingEntry{}
	values := []localValue{}
	roots := []string{}
	for _, selection := range selected {
		selection.file.ID = len(local)
		local = append(local, selection.file)
		root := filepath.Dir(selection.file.Path)
		home, _ := os.UserHomeDir()
		if root != filepath.Join(home, ".config") && root != filepath.Join(home, "Library", "Application Support") {
			roots = append(roots, root)
		}
		for _, value := range selection.values {
			settings = append(settings, settingEntry{selection.file.ID, value, selection.data})
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return errors.New("安全に編集できる設定項目がありません")
	}
	if len(values) > 100 {
		return errors.New("項目が100件を超えます。選択するファイルを絞ってください")
	}
	if appPath != "" {
		roots = append(roots, appPath)
	}
	paths := scanCodePaths(roots)
	if len(paths) > 300 {
		paths = paths[:300]
	}
	infos, err := describe(values, paths, request)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, value := range values {
		counts[fmt.Sprintf("%q", value.Path)]++
	}
	draft := map[int]any{}
	for {
		rows := make([]string, len(values))
		for number, value := range values {
			rows[number] = settingLabel(number, value, infos[number], counts[fmt.Sprintf("%q", value.Path)] > 1, draft[number])
		}
		index, err := selectSetting(reader, out, rows, tui)
		if err != nil {
			return err
		}
		if index < 0 {
			break
		}
		item := settings[index].value
		if counts[fmt.Sprintf("%q", item.Path)] > 1 {
			return errors.New("同名項目が複数ファイルにあるため編集できません。選択ファイルを絞ってください")
		}
		info := infos[index]
		if info.Description == "" {
			return errors.New("説明が取得できない項目は編集できません")
		}
		current := any(item.Value)
		if changed, ok := draft[index]; ok {
			current = changed
		}
		next, err := editSetting(reader, out, item, info, current)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(item.Value, next) {
			delete(draft, index)
		} else {
			draft[index] = next
		}
	}
	if len(draft) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(draft))
	for index := range draft {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	fields := []field{}
	changes := []pendingChange{}
	for _, index := range indexes {
		item := settings[index]
		fields = append(fields, field{Path: item.value.Path, Type: item.value.Type, Value: item.value.Value, Target: &target{ID: item.id, Path: item.value.Path}})
		changes = append(changes, pendingChange{ID: fmt.Sprintf("0:%d", len(fields)-1), Value: draft[index]})
	}
	files := []source{{Fields: fields}}
	plan, err := preparePlan(files, local)
	if err != nil {
		return err
	}
	for i, index := range indexes {
		setting := settings[index]
		if !bytes.Equal(setting.data, plan.Original[local[setting.id].Path]) || !reflect.DeepEqual(setting.value.Value, files[0].Fields[i].Value) {
			return errors.New("一覧表示後に設定ファイルが変わりました。再解析してください")
		}
	}
	previewID, _, err := plan.preview(changes)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		fmt.Fprintf(out, "%s: %s → %s\n", infos[index].Description, printable(settings[index].value.Value), printable(draft[index]))
	}
	ok, err := yes(reader, out, "この設定内容を適用しますか？")
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
