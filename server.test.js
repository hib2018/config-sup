import { test } from 'node:test';
import { deepStrictEqual, strictEqual, throws } from 'node:assert';
import { githubRepo, fieldsFromJson } from './server.js';

test('GitHub URL is strictly limited to a repository', () => {
  strictEqual(githubRepo('https://github.com/example/project.git'), 'https://github.com/example/project');
  for (const bad of ['https://github.com.evil.test/a/b', 'http://github.com/a/b', 'https://github.com/a/b/tree/main', 'https://github.com/a/.git', 'https://github.com/a/b?x=1']) throws(() => githubRepo(bad));
});
test('schema only includes editable primitive leaves', () => {
  deepStrictEqual(fieldsFromJson({ nested: { enabled: true, retries: 2, name: 'ok', unused: null }, list: [1] }), [
    { path: ['nested', 'enabled'], type: 'boolean', value: true },
    { path: ['nested', 'retries'], type: 'number', value: 2 },
    { path: ['nested', 'name'], type: 'string', value: 'ok' }
  ]);
});
