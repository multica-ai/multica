// @vitest-environment node

import {
  existsSync,
  readFileSync,
  readdirSync,
  statSync,
} from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const REPO_ROOT = resolve(
  fileURLToPath(new URL('../../..', import.meta.url))
);

const RETIRED_BROWSER_MODULES = [
  'apps/desktop/src/renderer/src/pages/ai-builder-session-page.tsx',
  'packages/core/agents/builder-protocol.ts',
  'packages/core/agents/mcp-support.ts',
  'packages/core/agents/openclaw-runtime-config.ts',
  'packages/core/agents/use-update-agent-allowlist.ts',
  'packages/views/agents/components/agent-access-settings.tsx',
  'packages/views/agents/components/create-agent-dialog.tsx',
  'packages/views/agents/components/tabs/agent-mcp-tab.tsx',
  'packages/views/agents/components/tabs/custom-args-tab.tsx',
  'packages/views/agents/components/tabs/env-file.ts',
  'packages/views/agents/components/tabs/env-tab.tsx',
  'packages/views/agents/components/tabs/integrations-tab.tsx',
  'packages/views/agents/components/tabs/mcp-config-tab.tsx',
  'packages/views/agents/components/tabs/runtime-config-tab.tsx',
  'packages/views/agents/create/ai-builder-session-page.tsx',
  'packages/views/agents/create/ai-create-agent-page.tsx',
  'packages/views/agents/create/builder-conversation.tsx',
  'packages/views/agents/create/builder-setup-panel.tsx',
  'packages/views/agents/create/builder-workspace.tsx',
  'packages/views/agents/create/choose-create-method-page.tsx',
  'packages/views/agents/create/squad-param.ts',
  'packages/views/agents/create/unfinished-drafts.tsx',
  'packages/views/agents/create/use-builder-draft-sync.ts',
  'packages/views/agents/create/use-builder-session.ts',
] as const;

const BROWSER_SOURCE_ROOTS = [
  'apps/web/app',
  'apps/desktop/src/renderer/src',
  'apps/tag-host/src',
  'packages/core',
  'packages/views',
] as const;

const RETIRED_BROWSER_CALLERS = [
  '/api/agent-builder/',
  'createAgentBuilderSession(',
  'listAgentBuilderSessions(',
  'saveAgentBuilderDraft(',
  'switchAgentBuilderRuntime(',
  'getAgentEnv(',
  'updateAgentEnv(',
  'listAgentMcpServers(',
  'addAgentMcpServer(',
  'setAgentMcpServerEnabled(',
  'removeAgentMcpServer(',
  'beginLarkInstall(',
  'getLarkInstallStatus(',
  'registerSlackBYO(',
  'registerDingTalkBYO(',
  'registerWecomBYO(',
  'updateDingTalkGroupRoute(',
] as const;

function productionSources(relativeRoot: string): string[] {
  const root = resolve(REPO_ROOT, relativeRoot);
  const sources: string[] = [];

  // Whole-product retirement may remove a source root entirely (for example,
  // Desktop). An absent root satisfies this no-caller contract.
  if (!existsSync(root)) return sources;

  for (const entry of readdirSync(root)) {
    const path = resolve(root, entry);
    const relativePath = path.slice(REPO_ROOT.length + 1);
    const stat = statSync(path);

    if (stat.isDirectory()) {
      if (entry === 'node_modules' || entry === 'dist' || entry === '.next') {
        continue;
      }
      sources.push(...productionSources(relativePath));
      continue;
    }

    if (
      /\.(?:ts|tsx)$/.test(entry) &&
      !/\.(?:test|spec)\.(?:ts|tsx)$/.test(entry) &&
      entry !== 'routeTree.gen.ts'
    ) {
      sources.push(path);
    }
  }

  return sources;
}

describe('Agent advanced-management source retirement', () => {
  it('keeps every physically retired browser module absent', () => {
    for (const modulePath of RETIRED_BROWSER_MODULES) {
      expect(existsSync(resolve(REPO_ROOT, modulePath)), modulePath).toBe(false);
    }
  });

  it('has no production browser caller for retired advanced APIs', () => {
    const sources = BROWSER_SOURCE_ROOTS.flatMap(productionSources);

    for (const caller of RETIRED_BROWSER_CALLERS) {
      const matches = sources.filter((path) =>
        readFileSync(path, 'utf8').includes(caller)
      );
      expect(matches, caller).toEqual([]);
    }
  });
});
