import { test } from 'node:test';
import { deepStrictEqual } from 'node:assert';
import { validateDescriptions } from './describe-agent.mjs';

test('descriptions omit unsupported options and invalid fields', () => {
  const items = [{ id: 0, path: ['mode'], type: 'string' }];
  const read = new Map([['/safe.ts', `enum Mode { Auto = 'auto', Manual = 'manual' }`]]);
  deepStrictEqual(validateDescriptions({ items: [
    { id: 0, path: ['mode'], description: '動作モード', values: ['auto', 'manual'], labels: ['自動', '手動'], evidencePath: '/safe.ts', quote: "Auto = 'auto', Manual = 'manual'" },
    { id: 1, description: '偽の項目' }
  ] }, items, read), [{ id: 0, description: '動作モード', choices: ['auto', 'manual'], choiceLabels: ['自動', '手動'] }]);
  deepStrictEqual(validateDescriptions({ items: [{ id: 0, path: ['mode'], description: '動作モード', values: ['auto', 'invented'], evidencePath: '/safe.ts', quote: "Auto = 'auto', Manual = 'manual'" }] }, items, read)[0].choices, []);
});
