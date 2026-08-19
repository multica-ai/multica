// @vitest-environment node

import { describe, expect, it } from 'vitest';
import {
  CHAT_WORKSPACE_TABS,
  TAG_WORKSPACE_SECTIONS,
  workspaceSwitchDestination,
} from './workspace-shell-model';

describe('Tag Workspace Shell navigation model', () => {
  it('exposes approved surfaces and removes merged or deleted entries', () => {
    const items = TAG_WORKSPACE_SECTIONS.flatMap((section) => section.items);

    expect(items.filter((item) => item.status === 'available')).toEqual([
      expect.objectContaining({ key: 'chat', path: 'chat' }),
      expect.objectContaining({ key: 'tasks', path: 'issues' }),
      expect.objectContaining({ key: 'agents', path: 'agents' }),
      expect.objectContaining({ key: 'runtimes', path: 'runtimes' }),
      expect.objectContaining({ key: 'settings', path: 'settings' }),
    ]);
    expect(items.map((item) => item.key)).not.toEqual(
      expect.arrayContaining([
        'files',
        'inbox',
        'my-tasks',
        'skills',
        'squads',
        'autopilots',
        'analytics',
      ])
    );
    expect(items).toContainEqual(
      expect.objectContaining({
        key: 'files',
        path: null,
        status: 'migrating',
      })
    );
    expect(items.filter((item) => item.status === 'migrating')).not.toHaveLength(
      0
    );
    expect(
      items
        .filter((item) => item.status === 'migrating')
        .every((item) => item.path === null)
    ).toBe(true);
  });

  it('keeps Files beside Chat instead of exposing it as a primary sidebar item', () => {
    expect(CHAT_WORKSPACE_TABS).toEqual([
      { key: 'chat', label: 'Chat', path: 'chat' },
      { key: 'files', label: 'Files', path: 'chat/files' },
    ]);
  });

  it('keeps the live module but clears Workspace-scoped object context when switching', () => {
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/chat?session=one')
    ).toBe('/studio-b/chat');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/chat/files')
    ).toBe('/studio-b/chat/files');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/issues/task-1')
    ).toBe('/studio-b/issues');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/projects')
    ).toBe('/studio-b/chat');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/agents/agent-1')
    ).toBe('/studio-b/agents');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/runtimes/machine-1')
    ).toBe('/studio-b/runtimes');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/settings?tab=mcp')
    ).toBe('/studio-b/settings');
  });
});
