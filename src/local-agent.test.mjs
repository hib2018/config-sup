import { test } from 'node:test';
import { deepStrictEqual, throws } from 'node:assert';
import { validateCandidates } from './local-agent.mjs';

test('agent cannot select unscanned paths or duplicate candidates', () => {
  const local = [{ id: 0 }, { id: 1 }];
  deepStrictEqual(validateCandidates({ candidates: [1, 1, 99, '0', 0] }, local), [1, 0]);
  throws(() => validateCandidates({ candidates: '0' }, local));
});
