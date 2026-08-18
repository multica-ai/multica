// @vitest-environment node

import { describe, expect, it, vi } from 'vitest';
import { createTagNavigationAdapter } from './tag-navigation-provider';

describe('TagNavigationProvider adapter contract', () => {
  it('supports pathname, search, push, replace, history, internal links, and share links', () => {
    const navigate = vi.fn();
    const back = vi.fn();
    const forward = vi.fn();
    const open = vi.fn();
    const adapter = createTagNavigationAdapter({
      location: {
        pathname: '/tag/design-lab/chat',
        search: '?session=session-1',
      },
      origin: 'http://localhost:3000',
      navigate,
      back,
      forward,
      open,
      canGoBack: () => true,
    });

    expect(adapter.pathname).toBe('/design-lab/chat');
    expect(adapter.searchParams.get('session')).toBe('session-1');
    expect(adapter.canGoBack?.()).toBe(true);
    expect(adapter.getShareableUrl('/design-lab/chat?session=session-1')).toBe(
      'http://localhost:3000/tag/design-lab/chat?session=session-1'
    );
    expect(adapter.resolveHref?.('/design-lab/issues/issue-1')).toBe(
      '/tag/design-lab/issues/issue-1'
    );

    adapter.push('/design-lab/issues');
    adapter.replace('/design-lab/chat?session=session-2');
    adapter.back();
    adapter.forward?.();
    adapter.openInNewTab?.('/design-lab/chat');

    expect(navigate).toHaveBeenNthCalledWith(1, '/tag/design-lab/issues', false);
    expect(navigate).toHaveBeenNthCalledWith(
      2,
      '/tag/design-lab/chat?session=session-2',
      true
    );
    expect(back).toHaveBeenCalledOnce();
    expect(forward).toHaveBeenCalledOnce();
    expect(open).toHaveBeenCalledWith('/tag/design-lab/chat');
  });
});
