// @vitest-environment jsdom

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

const openCreateTask = vi.hoisted(() => vi.fn());

vi.mock('@multica/core/issues/stores/create-mode-store', () => ({
  openCreateIssueWithPreference: openCreateTask,
}));

import { TaskCreateButton } from './task-create-button';

describe('TaskCreateButton', () => {
  it('opens Multica original task creation flow', () => {
    render(<TaskCreateButton />);

    fireEvent.click(screen.getByRole('button', { name: 'Create Task' }));

    expect(openCreateTask).toHaveBeenCalledOnce();
  });
});
