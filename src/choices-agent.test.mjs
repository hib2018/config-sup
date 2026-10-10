import { test } from 'node:test';
import { deepStrictEqual } from 'node:assert';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { codeTools, verifiedChoices, sensitiveSource } from './choices-agent.mjs';

test('credentials in source are never returned to the model tool', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'config-sup-secret-'));
  try {
    const path = join(dir, 'settings.ts');
    await writeFile(path, `const apiKey = 'sk-example1234567890123456';`);
    const { read, tools } = codeTools([path]);
    const result = await tools[1].execute('', { path });
    deepStrictEqual(read.size, 0);
    deepStrictEqual(result.content[0].text, 'Sensitive-looking source was excluded');
    deepStrictEqual(sensitiveSource('safe choices = ["auto", "manual"]'), false);
    deepStrictEqual(sensitiveSource('const key = "opaque"'), true);
    deepStrictEqual(sensitiveSource('A'.repeat(45)), true);
  } finally { await rm(dir, { recursive: true, force: true }); }
});

test('choice evidence must come from an actually read allowlisted source', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'config-sup-choice-'));
  try {
    const path = join(dir, 'settings.ts');
    await writeFile(path, `const choices = ['auto', 'manual'];`);
    const { read, tools } = codeTools([path]);
    deepStrictEqual(JSON.parse((await tools[0].execute('', { query: 'settings' })).content[0].text).paths, [path]);
    await tools[1].execute('', { path });
    const item = { path: ['mode'], type: 'string' };
    const proposal = { choices: [{ path: ['mode'], values: ['auto', 'manual'], evidencePath: path, quote: "['auto', 'manual']" }] };
    deepStrictEqual(verifiedChoices(proposal, item, read), ['auto', 'manual']);
    deepStrictEqual(verifiedChoices({ choices: [{ ...proposal.choices[0], values: ['auto', 'invented'] }] }, item, read), []);
    deepStrictEqual(verifiedChoices({ choices: [{ ...proposal.choices[0], evidencePath: '/tmp/elsewhere' }] }, item, read), []);
  } finally { await rm(dir, { recursive: true, force: true }); }
});
