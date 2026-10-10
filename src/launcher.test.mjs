import { test } from 'node:test';
import { match, strictEqual } from 'node:assert';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, symlinkSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

test('dedicated command resolves project through a symlink outside the repository', () => {
  const temp = mkdtempSync(join(tmpdir(), 'config-sup-launch-'));
  try {
    const bin = join(temp, 'bin');
    mkdirSync(bin);
    const command = join(bin, 'config-sup');
    symlinkSync(resolve('bin/config-sup'), command);
    const result = spawnSync(command, [], { cwd: temp, encoding: 'utf8', timeout: 20_000 });
    strictEqual(result.status, 1);
    match(result.stderr, /承認を伴うため、対話端末から実行してください/);
  } finally { rmSync(temp, { recursive: true, force: true }); }
});
