import YAML, { isMap, isScalar } from 'yaml';
import * as TOML from '@iarna/toml';

const typeOf = value => typeof value === 'string' || typeof value === 'boolean' ? typeof value : typeof value === 'number' && Number.isFinite(value) && Math.abs(value) <= Number.MAX_SAFE_INTEGER ? 'number' : null;
const safePath = path => path.every(key => typeof key === 'string' && key && !/auth|token|secret|credential|password|keychain|session/i.test(key) && !['__proto__','constructor','prototype'].includes(key));
const jsonValue = value => JSON.stringify(value);
const equalPath = (a, b) => JSON.stringify(a) === JSON.stringify(b);

function yamlDocument(text) {
  const docs = YAML.parseAllDocuments(text, { uniqueKeys: true });
  if (docs.length !== 1 || docs[0].errors.length || !isMap(docs[0].contents)) throw Error('対応できないYAML文書です');
  return docs[0];
}
function yamlScalars(text) {
  const doc = yamlDocument(text), fields = [];
  const walk = (map, prefix) => {
    for (const pair of map.items) {
      if (!isScalar(pair.key) || typeof pair.key.value !== 'string') continue;
      const path = [...prefix, pair.key.value];
      if (!safePath(path)) continue;
      if (isMap(pair.value)) walk(pair.value, path);
      else if (isScalar(pair.value) && typeOf(pair.value.value) && !pair.value.anchor && !['BLOCK_LITERAL','BLOCK_FOLDED'].includes(pair.value.type) && pair.value.range) fields.push({ path, type: typeOf(pair.value.value), value: pair.value.value, start: pair.value.range[0], end: pair.value.range[1] });
    }
  };
  walk(doc.contents, []);
  return fields;
}

// Conservative TOML locator: only simple scalar assignments in ordinary tables.
// Multiline strings and arrays of tables are rejected, never rewritten speculatively.
function tomlScalars(text) {
  if (text.includes('"""') || text.includes("'''")) throw Error('複数行文字列を含むTOMLは未対応です');
  const parsed = TOML.parse(text);
  const fields = [], lines = text.match(/[^\n]*\n|[^\n]+$/g) || [];
  let table = [], offset = 0;
  for (const line of lines) {
    const header = /^\s*\[([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*)\]\s*(?:#.*)?\r?\n?$/.exec(line);
    if (header) table = header[1].split('.');
    else if (/^\s*\[/.test(line)) table = null;
    else if (table) {
      const match = /^(\s*)([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*)(\s*=\s*)(.*?)(\r?\n?)$/.exec(line);
      if (match) {
        const path = [...table, ...match[2].split('.')];
        if (safePath(path)) {
          let quote = '', escaped = false, comment = match[4].length;
          for (let i = 0; i < match[4].length; i++) {
            const ch = match[4][i];
            if (escaped) { escaped = false; continue; }
            if (ch === '\\' && quote === '"') { escaped = true; continue; }
            if (quote && ch === quote) { quote = ''; continue; }
            if (!quote && (ch === '"' || ch === "'")) { quote = ch; continue; }
            if (!quote && ch === '#') { comment = i; break; }
          }
          const raw = match[4].slice(0, comment).trimEnd();
          const scalar = /^(?:"(?:[^"\\]|\\.)*"|'[^']*'|true|false|[+-]?(?:\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?\d+)?|inf|nan))$/i.test(raw);
          let value = parsed;
          for (const key of path) value = value?.[key];
          if (scalar && typeOf(value)) {
            const start = offset + match[1].length + match[2].length + match[3].length;
            fields.push({ path, type: typeOf(value), value, start, end: start + raw.length });
          }
        }
      }
    }
    offset += line.length;
  }
  return fields;
}
export function inspect(format, text) {
  const fields = format === 'yaml' ? yamlScalars(text) : format === 'toml' ? tomlScalars(text) : (() => { throw Error('未対応の形式です'); })();
  return fields.map(({ path, type, value }) => ({ path, type, value }));
}
export function patch(format, text, changes) {
  if (!Array.isArray(changes) || !changes.length) throw Error('変更がありません');
  const fields = format === 'yaml' ? yamlScalars(text) : tomlScalars(text);
  const edits = [];
  for (const change of changes) {
    const matches = fields.filter(field => equalPath(field.path, change.path));
    if (matches.length !== 1 || matches[0].type !== typeOf(change.value)) throw Error('対象のキーまたは型が一致しません');
    if (edits.some(edit => edit.start === matches[0].start)) throw Error('同じキーを複数回変更できません');
    edits.push({ start: matches[0].start, end: matches[0].end, replacement: jsonValue(change.value) });
  }
  let result = text;
  for (const edit of edits.sort((a, b) => b.start - a.start)) result = result.slice(0, edit.start) + edit.replacement + result.slice(edit.end);
  const after = inspect(format, result);
  for (const change of changes) {
    if (!after.some(field => equalPath(field.path, change.path) && field.value === change.value)) throw Error('変更後の解析結果が一致しません');
  }
  return result;
}

if (process.argv[1]?.endsWith('/local-format.mjs')) {
  try {
    let data = '';
    for await (const chunk of process.stdin) { data += chunk; if (data.length > 300_000) throw Error('入力が大きすぎます'); }
    const { action, format, text, changes } = JSON.parse(data);
    process.stdout.write(JSON.stringify(action === 'inspect' ? inspect(format, text) : action === 'patch' ? patch(format, text, changes) : null));
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
