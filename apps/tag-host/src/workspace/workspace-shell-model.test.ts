// @vitest-environment node

import { describe, expect, it } from 'vitest';
import {
  TAG_WORKSPACE_SECTIONS,
  workspaceSwitchDestination,
} from './workspace-shell-model';

describe('Tag Workspace Shell navigation model', () => {
  it('keeps existing Chat and Tasks live while every later module stays visibly non-navigable', () => {
    const items = TAG_WORKSPACE_SECTIONS.flatMap((section) => section.items);

    expect(items.filter((item) => item.status === 'available')).toEqual([
      expect.objectContaining({ key: 'chat', path: 'chat' }),
      expect.objectContaining({ key: 'tasks', path: 'issues' }),
    ]);
    expect(items.filter((item) => item.status === 'migrating')).not.toHaveLength(
      0
    );
    expect(
      items
        .filter((item) => item.status === 'migrating')
        .every((item) => item.path === null)
    ).toBe(true);
  });

  it('keeps the live module but clears Workspace-scoped object context when switching', () => {
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/chat?session=one')
    ).toBe('/studio-b/chat');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/issues/task-1')
    ).toBe('/studio-b/issues');
    expect(
      workspaceSwitchDestination('studio-b', '/studio-a/projects')
    ).toBe('/studio-b/chat');
  });
});
