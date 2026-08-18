// @vitest-environment node

import { describe, expect, it } from 'vitest';
import { getRouter } from './router';

describe('Tag Host task routes', () => {
  it('exposes the reused Multica task list and detail inside a workspace', () => {
    const routeIds = Object.keys(getRouter().routesById);

    expect(routeIds).toEqual(
      expect.arrayContaining([
        '/$workspaceSlug/issues',
        '/$workspaceSlug/issues/$issueId',
      ])
    );
  });
});
