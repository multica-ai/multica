// @vitest-environment node

import { describe, expect, it } from 'vitest';
import { resolveTagSessionRecovery } from './tag-session-recovery';

describe('Tag session recovery', () => {
  it('returns stale or invalid Multica sessions to the VIBES-owned handoff', () => {
    expect(
      resolveTagSessionRecovery({
        isLoading: false,
        hasUser: false,
        workspaceSlug: 'design-lab',
        pathname: '/tag/design-lab/issues/task-1',
        search: '?view=activity',
        hash: '#comment-3',
      })
    ).toBe(
      '/tag-entry?workspace=design-lab&page=%2Fissues%2Ftask-1%3Fview%3Dactivity%23comment-3'
    );
  });

  it('does not interrupt auth bootstrap or an authenticated deep link', () => {
    for (const input of [
      { isLoading: true, hasUser: false },
      { isLoading: false, hasUser: true },
    ]) {
      expect(
        resolveTagSessionRecovery({
          ...input,
          workspaceSlug: 'design-lab',
          pathname: '/tag/design-lab/chat',
          search: '',
          hash: '',
        })
      ).toBeNull();
    }
  });
});
