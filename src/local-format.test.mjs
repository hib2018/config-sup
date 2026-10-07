import { test } from 'node:test';
import { deepStrictEqual, strictEqual, throws } from 'node:assert';
import { inspect, patch } from './local-format.mjs';

test('YAML edits only a scalar, retaining comments and unrelated structure', () => {
  const text = '# note\nmode: auto # keep\nnested:\n  enabled: true\n  secret: hidden\nlist: [a, b]\n';
  const fields = inspect('yaml', text);
  deepStrictEqual(fields.map(f => f.path), [['mode'], ['nested', 'enabled']]);
  strictEqual(patch('yaml', text, [{ path: ['mode'], value: 'manual' }, { path: ['nested','enabled'], value: false }]), '# note\nmode: "manual" # keep\nnested:\n  enabled: false\n  secret: hidden\nlist: [a, b]\n');
  throws(() => patch('yaml', text, [{ path: ['list'], value: 'oops' }]));
  throws(() => inspect('yaml', 'a: 1\na: 2\n'));
});
test('TOML edits only simple scalar values and refuses ambiguous syntax', () => {
  const text = '# note\nmode = "auto" # keep\n[next]\nenabled = true\nbig = 9007199254740993\n';
  deepStrictEqual(inspect('toml', text).map(f => f.path), [['mode'], ['next','enabled']]);
  strictEqual(patch('toml', text, [{ path: ['mode'], value: 'manual' }, { path: ['next','enabled'], value: false }]), '# note\nmode = "manual" # keep\n[next]\nenabled = false\nbig = 9007199254740993\n');
  throws(() => inspect('toml', 'msg = """long\ntext"""\n'));
  throws(() => patch('toml', text, [{ path: ['next','missing'], value: true }]));
});
