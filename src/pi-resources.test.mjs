import { test } from 'node:test';
import { deepStrictEqual, strictEqual } from 'node:assert';
import { createAgentSession, SessionManager } from '@earendil-works/pi-coding-agent';
import { localTools, resources } from './pi-resources.mjs';

test('local-only agent tools expose names and types but not values', async () => {
  const local = [{ id: 0, path: '/home/user/.config/zed/settings.json', fields: [{ path: ['enabled'], type: 'boolean', value: 'private value' }] }];
  const tools = localTools(local);
  const { session } = await createAgentSession({ resourceLoader: resources, sessionManager: SessionManager.inMemory(), tools: ['search_local', 'inspect_local'], customTools: tools });
  try { deepStrictEqual(session.getActiveToolNames().sort(), ['inspect_local', 'search_local']); }
  finally { session.dispose(); }
  const found = JSON.parse((await tools[0].execute('', { query: 'zed' })).content[0].text);
  deepStrictEqual(found.files, [{ id: 0, path: local[0].path }]);
  const keys = JSON.parse((await tools[1].execute('', { id: 0 })).content[0].text);
  deepStrictEqual(keys.fields, [{ path: ['enabled'], type: 'boolean' }]);
  strictEqual(JSON.stringify(keys).includes('private value'), false);
});
