export {};
type Value = string | number | boolean;
type Field = { path: string[]; type: 'string' | 'number' | 'boolean'; value: Value; choices?: Value[]; target?: { id: number; path: string[]; file: string } };
type Source = { file: string; fields: Field[] };
type Analysis = { repo: string; files: Source[]; descriptions: Record<string, string>; token: string };
const byId = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const status = byId<HTMLParagraphElement>('status');
const editor = byId<HTMLElement>('editor');
const fieldsNode = byId<HTMLDivElement>('fields');
const review = byId<HTMLElement>('review');
const previewButton = byId<HTMLButtonElement>('preview');
let analysis: Analysis;
let updates = new Map<string, Value>();
let previewId = '';
let revision = 0;
let controls: (HTMLInputElement | HTMLSelectElement)[] = [];

function message(text: string, error = false) { status.textContent = text; status.classList.toggle('error', error); }
function resetReview() { revision++; previewId = ''; review.hidden = true; previewButton.disabled = !updates.size || controls.some(control => !control.checkValidity()); }
async function post(path: string, input: unknown) {
  const response = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) });
  const result = await response.json();
  if (!response.ok) throw Error(result.error || '処理に失敗しました');
  return result;
}
function render() {
  updates = new Map(); controls = []; fieldsNode.replaceChildren(); resetReview();
  analysis.files.forEach((source, fi) => {
    const heading = document.createElement('h2'); heading.textContent = source.file; fieldsNode.append(heading);
    source.fields.forEach((field, ki) => {
      const id = `${fi}:${ki}`;
      const path = field.path.join('.');
      const div = document.createElement('div'); div.className = 'field';
      const label = document.createElement('label'); label.textContent = path; div.append(label);
      const destination = document.createElement('small');
      destination.textContent = field.target ? `適用先: ${field.target.file} → ${field.target.path.join('.')}` : '適用先未特定（編集不可）';
      div.append(destination);
      const description = analysis.descriptions[`${source.file}|${path}`];
      if (description) { const line = document.createElement('p'); line.className = 'description'; line.textContent = description; div.append(line); }
      if (field.target) {
        const choices = field.type === 'boolean' ? [true, false] : field.choices && field.choices.length >= 2 ? field.choices : undefined;
        let control: HTMLInputElement | HTMLSelectElement;
        if (choices) {
          const select = document.createElement('select');
          const values = choices.some(value => value === field.value) ? choices : [field.value, ...choices];
          for (const value of values) { const option = document.createElement('option'); option.value = JSON.stringify(value); option.textContent = typeof value === 'boolean' ? value ? '有効 (true)' : '無効 (false)' : String(value); select.append(option); }
          select.value = JSON.stringify(field.value);
          control = select;
        } else {
          const hint = document.createElement('small'); hint.textContent = field.type === 'number' ? '数値を入力' : '自由入力（文字列）'; div.append(hint);
          const input = document.createElement('input'); input.type = field.type === 'number' ? 'number' : 'text';
          if (field.type === 'number') input.step = 'any';
          input.required = true; input.value = String(field.value); control = input;
        }
        label.htmlFor = `setting-${fi}-${ki}`; control.id = label.htmlFor;
        control.addEventListener('input', () => {
          const value: Value = control instanceof HTMLSelectElement ? JSON.parse(control.value) : field.type === 'number' ? control.valueAsNumber : control.value;
          if (!control.checkValidity() || (typeof value === 'number' && !Number.isFinite(value))) updates.delete(id);
          else if (value === field.value) updates.delete(id);
          else updates.set(id, value);
          resetReview();
        });
        controls.push(control); div.append(control);
      }
      fieldsNode.append(div);
    });
  });
}
byId<HTMLFormElement>('analyze').addEventListener('submit', async event => {
  event.preventDefault(); message('解析中…'); editor.hidden = true; resetReview();
  try {
    analysis = await post('/analyze', { url: byId<HTMLInputElement>('url').value, useAgent: byId<HTMLInputElement>('agent').checked, useLocal: byId<HTMLInputElement>('local').checked });
    editor.hidden = false; render(); message(`${analysis.files.length} 件の設定ファイルを解析しました`);
  } catch (error) { message(String(error), true); }
});
previewButton.addEventListener('click', async () => {
  const currentRevision = revision; previewButton.disabled = true;
  try {
    const result = await post('/preview', { token: analysis.token, changes: [...updates].map(([id, value]) => ({ id, value })) });
    if (currentRevision !== revision) return;
    previewId = result.previewId; byId('diff').textContent = result.diff; review.hidden = false; message('適用先と差分を確認してください');
  } catch (error) { message(String(error), true); }
  finally { previewButton.disabled = !updates.size || controls.some(control => !control.checkValidity()); }
});
byId<HTMLButtonElement>('apply').addEventListener('click', async () => {
  if (!previewId || review.hidden) return;
  const id = previewId; previewId = ''; byId<HTMLButtonElement>('apply').disabled = true;
  try {
    await post('/apply', { token: analysis.token, previewId: id });
    for (const control of controls) control.disabled = true;
    previewButton.disabled = true; review.hidden = true; message('適用しました。続けるには再解析してください');
  } catch (error) { review.hidden = true; message(String(error), true); }
  finally { byId<HTMLButtonElement>('apply').disabled = false; }
});
