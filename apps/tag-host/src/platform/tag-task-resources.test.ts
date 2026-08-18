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
    };
    const multicaCopy = RESOURCES.en as {
      issues: { page: { breadcrumb_title: string } };
    };

    expect(taskCopy.issues.page.breadcrumb_title).toBe('Tasks');
    expect(taskCopy.issues.table.quick_create_placeholder).toBe(
      'Add a task…'
    );
    expect(taskCopy.modals.create_issue.submit).toBe('Create Task');
    expect(multicaCopy.issues.page.breadcrumb_title).toBe('Issues');
  });
});
