import { test } from 'node:test';
import { strictEqual, deepStrictEqual, rejects } from 'node:assert';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { repoTools, localTools, normalizeResult, resources } from './agent.mjs';
import { createAgentSession, SessionManager } from '@earendil-works/pi-coding-agent';

test('agent can search and read tracked JSONC without executing it', async () => {
  const root = await mkdtemp(join(tmpdir(), 'config-sup-agent-test-'));
  try {
    execFileSync('git', ['init', '-q', root]);
    await mkdir(join(root, '.zed'));
    await writeFile(join(root, '.zed', 'settings.json'), '{ // comment\n"enabled": true,\n}');
    await writeFile(join(root, 'untracked.txt'), 'private');
    execFileSync('git', ['-C', root, 'add', '.zed/settings.json']);
    execFileSync('git', ['-C', root, '-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'test']);
    const { tracked, tools } = await repoTools(root);
    const local = [{ id: 0, path: '/home/user/.config/zed/settings.json', fields: [{ path: ['enabled'], type: 'boolean' }] }];
    const { session } = await createAgentSession({ resourceLoader: resources, sessionManager: SessionManager.inMemory(), tools: ['search_paths', 'read_source', 'search_local', 'inspect_local'], customTools: [...tools, ...localTools(local)] });
    try { deepStrictEqual(session.getActiveToolNames().sort(), ['inspect_local', 'read_source', 'search_local', 'search_paths']); }
    finally { session.dispose(); }
    const search = JSON.parse((await tools[0].execute('', { query: 'settings' })).content[0].text);
    deepStrictEqual(search.paths, ['.zed/settings.json']);
    strictEqual((await tools[1].execute('', { path: 'untracked.txt' })).content[0].text, 'File is not tracked or is not a regular file');
    strictEqual((await tools[1].execute('', { path: '.zed/settings.json' })).content[0].text.includes('// comment'), true);
    const found = JSON.parse((await localTools(local)[0].execute('', { query: 'zed' })).content[0].text);
    deepStrictEqual(found.files, [{ id: 0, path: local[0].path }]);
    const keys = JSON.parse((await localTools(local)[1].execute('', { id: 0 })).content[0].text);
    deepStrictEqual(keys.fields, [{ path: ['enabled'], type: 'boolean' }]);
    const result = normalizeResult({ files: [{ file: '.zed/settings.json', fields: [
      { path: ['enabled'], type: 'boolean', value: true, description: 'オン', choices: [true, false], target: { id: 0, path: ['enabled'] } },
      { path: ['fake'], type: 'boolean', value: 'wrong' },
      { path: ['__proto__'], type: 'string', value: 'bad' }
    ] }, { file: 'untracked.txt', fields: [{ path: ['a'], type: 'string', value: 'bad' }] }] }, tracked, local);
    deepStrictEqual(result.files, [{ file: '.zed/settings.json', fields: [{ path: ['enabled'], type: 'boolean', value: true, target: { id: 0, path: ['enabled'] }, choices: [true, false] }] }]);
    strictEqual(result.descriptions['.zed/settings.json|enabled'], 'オン');
    await rejects(async () => normalizeResult({ files: [] }, tracked));
  } finally { await rm(root, { recursive: true, force: true }); }
});
