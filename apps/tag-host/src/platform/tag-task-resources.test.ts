// @vitest-environment node

import { describe, expect, it } from 'vitest';
import { RESOURCES } from '@multica/views/locales';
import { createTagTaskResources } from './tag-task-resources';

describe('VIBES Tag task vocabulary', () => {
  it('adapts user-visible issue wording without mutating Multica resources', () => {
    const resources = createTagTaskResources(RESOURCES.en);
    const taskCopy = resources as {
      issues: {
        page: { breadcrumb_title: string };
        table: { quick_create_placeholder: string };
      };
      modals: { create_issue: { submit: string } };
      projects: {
        detail: { toast_move_issue_failed: string };
      };
      runtimes: { machine: { metrics: { health_clear: string } } };
      settings: { lark: { page_description: string } };
    };
    const multicaCopy = RESOURCES.en as {
      issues: { page: { breadcrumb_title: string } };
    };

    expect(taskCopy.issues.page.breadcrumb_title).toBe('Tasks');
    expect(taskCopy.issues.table.quick_create_placeholder).toBe(
      'Add a task…'
    );
    expect(taskCopy.modals.create_issue.submit).toBe('Create Task');
    expect(taskCopy.projects.detail.toast_move_issue_failed).toBe(
      'Failed to move task'
    );
    expect(taskCopy.runtimes.machine.metrics.health_clear).toBe('No issues');
    expect(taskCopy.settings.lark.page_description).toContain('/issue');
    expect(multicaCopy.issues.page.breadcrumb_title).toBe('Issues');
  });
});
