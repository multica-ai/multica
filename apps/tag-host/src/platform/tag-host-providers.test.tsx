// @vitest-environment jsdom

import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

const captured = vi.hoisted(() => ({ props: null as Record<string, unknown> | null }));
const localeAdapter = vi.hoisted(() => ({
  getUserChoice: () => 'zh-Hans',
  getSystemPreferences: () => ['en'],
  persist: vi.fn(),
}));

vi.mock('@multica/core/platform', () => ({
  CoreProvider: ({ children, ...props }: { children: React.ReactNode } & Record<string, unknown>) => {
    captured.props = props;
    return <>{children}</>;
  },
}));
vi.mock('@multica/core/i18n/browser', () => ({
  createBrowserCookieLocaleAdapter: () => localeAdapter,
}));
vi.mock('@multica/views/locales', () => ({
  RESOURCES: {
    en: { chat: { title: 'Chat' } },
    'zh-Hans': { chat: { title: '聊天' } },
  },
}));
vi.mock('@multica/ui/components/common/theme-provider', () => ({
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="theme-provider">{children}</div>
  ),
}));
vi.mock('./paths', () => ({
  resolveTagRuntimeUrls: () => ({ apiBaseUrl: '/api/tag', wsUrl: 'ws://localhost/ws/tag' }),
}));
vi.mock('./tag-navigation-provider', () => ({
  TagNavigationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./tag-task-resources', () => ({ createTagTaskResources: (resources: unknown) => resources }));

import { TagHostProviders } from './tag-host-providers';

describe('TagHostProviders', () => {
  it('preserves the original theme and locale providers for Tag Chat', () => {
    render(<TagHostProviders><div>Workspace</div></TagHostProviders>);

    expect(screen.getByTestId('theme-provider')).toBeTruthy();
    expect(captured.props?.locale).toBe('zh-Hans');
    expect(captured.props?.localeAdapter).toBe(localeAdapter);
    expect(Object.keys(captured.props?.resources as object)).toEqual(['en', 'zh-Hans']);
  });
});
