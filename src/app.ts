export {};
type Field = { path: string[]; type: 'string' | 'number' | 'boolean'; value: string | number | boolean };
type Source = { file: string; fields: Field[] };
type Analysis = { repo: string; files: Source[]; descriptions: Record<string, string>; agent: string };
type LocalHandle = { name: string; getFile(): Promise<File>; createWritable(): Promise<{ write(data: string): Promise<void>; close(): Promise<void>; abort(): Promise<void> }> };
declare global { interface Window { showOpenFilePicker?: (options: object) => Promise<LocalHandle[]> } }

const byId = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const status = byId<HTMLParagraphElement>('status');
const editor = byId<HTMLElement>('editor');
const sourceSelect = byId<HTMLSelectElement>('source');
const fieldsNode = byId<HTMLDivElement>('fields');
const review = byId<HTMLElement>('review');
const previewButton = byId<HTMLButtonElement>('preview');
let analysis: Analysis;
let handle: LocalHandle | undefined;
let original = '';
let local: Record<string, unknown>;
let updates = new Map<string, string | number | boolean>();
let proposed = '';

function message(text: string, error = false) { status.textContent = text; status.classList.toggle('error', error); }
function source() { return analysis.files[Number(sourceSelect.value)]; }
function getPath(object: unknown, path: string[]): unknown {
  let current = object;
  for (const key of path) {
    if (!current || typeof current !== 'object' || Array.isArray(current) || !Object.hasOwn(current, key)) return undefined;
    current = (current as Record<string, unknown>)[key];
  }
  return current;
}
function setPath(object: Record<string, unknown>, path: string[], value: unknown) {
  let current = object;
  for (const key of path.slice(0, -1)) current = current[key] as Record<string, unknown>;
  current[path.at(-1)!] = value;
}
function resetReview() { review.hidden = true; proposed = ''; }
function render() {
  updates = new Map(); resetReview(); fieldsNode.replaceChildren();
  const selected = source();
  for (const field of selected.fields) {
    const path = field.path.join('.');
    const existing = handle ? getPath(local, field.path) : field.value;
    const compatible = !handle || typeof existing === field.type;
    const div = document.createElement('div'); div.className = 'field';
    const label = document.createElement('label'); label.textContent = path; div.append(label);
    const note = document.createElement('small');
    note.textContent = `${selected.file} → ${path}${analysis.descriptions[`${selected.file}|${path}`] ? ` · AIによる説明（要確認）: ${analysis.descriptions[`${selected.file}|${path}`]}` : ''}${compatible ? '' : ' · 適用先に同じ型の項目がないため編集不可'}`;
    div.append(note);
    const input = document.createElement('input'); input.disabled = !handle || !compatible;
    if (field.type === 'boolean') {
      input.type = 'checkbox'; input.checked = existing as boolean;
    } else {
      input.type = field.type === 'number' ? 'number' : 'text';
      if (field.type === 'number') input.step = 'any';
      input.value = String(existing ?? '');
    }
    input.setAttribute('aria-label', path);
    input.addEventListener('input', () => {
      resetReview();
      const value = field.type === 'boolean' ? input.checked : field.type === 'number' ? input.valueAsNumber : input.value;
      if (typeof value === 'number' && !Number.isFinite(value)) updates.delete(JSON.stringify(field.path));
      else updates.set(JSON.stringify(field.path), value);
      previewButton.disabled = !updates.size;
    });
    div.append(input); fieldsNode.append(div);
  }
  previewButton.disabled = true;
}

byId<HTMLFormElement>('analyze').addEventListener('submit', async event => {
  event.preventDefault(); message('解析中…'); editor.hidden = true; handle = undefined;
  try {
    const response = await fetch('/analyze', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url: byId<HTMLInputElement>('url').value, useAgent: byId<HTMLInputElement>('agent').checked }) });
    const data = await response.json();
    if (!response.ok) throw Error(data.error || '解析に失敗しました');
    analysis = data;
    sourceSelect.replaceChildren(...analysis.files.map((file, index) => { const option = document.createElement('option'); option.value = String(index); option.textContent = file.file; return option; }));
    byId('agent-status').textContent = analysis.agent;
    byId('destination').textContent = '';
    editor.hidden = false; render(); message(`${analysis.files.length} 件の設定ファイルを発見しました`);
  } catch (error) { message(String(error), true); }
});
sourceSelect.addEventListener('change', render);
byId<HTMLButtonElement>('pick').addEventListener('click', async () => {
  if (!window.showOpenFilePicker) { message('このブラウザはファイルへの書き込みに非対応です。ChromeまたはEdgeを使用してください', true); return; }
  try {
    const [candidate] = await window.showOpenFilePicker({ multiple: false, types: [{ description: 'JSON', accept: { 'application/json': ['.json'] } }] });
    const text = await (await candidate.getFile()).text();
    if (text.length > 100_000) throw Error('適用先が大きすぎます');
    const parsed = JSON.parse(text);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw Error('JSONオブジェクトを選択してください');
    handle = candidate; original = text; local = parsed;
    byId('destination').textContent = `適用先: ${candidate.name}`;
    render(); message('適用先を選択しました');
  } catch (error) { if ((error as Error).name !== 'AbortError') message(String(error), true); }
});
previewButton.addEventListener('click', () => {
  try {
    const updated = structuredClone(local);
    for (const [key, value] of updates) setPath(updated, JSON.parse(key), value);
    proposed = JSON.stringify(updated, null, 2) + '\n';
    if (JSON.stringify(updated) === JSON.stringify(local)) throw Error('変更がありません');
    byId('diff').textContent = `変更前（選択した項目）:\n${[...updates].map(([key]) => `${JSON.parse(key).join('.')} = ${JSON.stringify(getPath(local, JSON.parse(key)))}`).join('\n')}\n\n変更後（選択した項目）:\n${[...updates].map(([key, value]) => `${JSON.parse(key).join('.')} = ${JSON.stringify(value)}`).join('\n')}\n\n注意: 適用時はJSONファイル全体を整形して書き直します。`;
    review.hidden = false; message('差分を確認してから適用してください');
  } catch (error) { resetReview(); message(String(error), true); }
});
byId<HTMLButtonElement>('apply').addEventListener('click', async () => {
  if (!handle || !proposed || review.hidden) return;
  try {
    if (await (await handle.getFile()).text() !== original) throw Error('適用先が変更されています。ファイルを選び直してください');
    const writer = await handle.createWritable();
    try { await writer.write(proposed); await writer.close(); }
    catch (error) { await writer.abort(); throw error; }
    original = proposed; local = JSON.parse(proposed); render(); message('適用しました');
  } catch (error) { resetReview(); message(String(error), true); }
});
