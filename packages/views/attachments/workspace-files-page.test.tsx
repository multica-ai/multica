import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { WorkspaceFile } from '@multica/core/types';
import { WorkspaceFilesPage } from './workspace-files-page';

const tryOpen = vi.fn(() => true);
const download = vi.fn(async () => {});
let response: { attachments: WorkspaceFile[]; hasMore: boolean; nextOffset: number | null };
let shouldFail = false;

vi.mock('@multica/core/attachments', () => ({
  workspaceFilesOptions: () => ({
    queryKey: ['attachments', 'workspace', 'ws-1'],
    queryFn: async () => {
      if (shouldFail) throw new Error('network unavailable');
      return response;
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage: typeof response) => lastPage.nextOffset ?? undefined,
  }),
}));

vi.mock('@multica/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@multica/core/paths', () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/studio/issues/${id}`,
    chatSession: (id: string) => `/studio/chat?session=${id}`,
  }),
}));

vi.mock('../editor', () => ({
  isPreviewable: () => true,
  useAttachmentPreview: () => ({ tryOpen, open: vi.fn(), modal: null }),
  useDownloadAttachment: () => download,
}));

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (selector: (resources: Record<string, unknown>) => string) =>
      selector({
        files: {
          title: 'Files',
          description: 'Shared files from Chat and Issues',
          empty_title: 'No shared files yet',
          empty_description: 'Files attached in Chat or Issues will appear here.',
          load_failed: 'Files could not be loaded.',
          retry: 'Retry',
          preview: 'Preview',
          download: 'Download',
          source_chat: 'Chat',
          source_issue: 'Issue',
          untitled_chat: 'Untitled chat',
          load_more: 'Load more',
          loading_more: 'Loading...',
          loading: 'Loading files',
          list_label: 'Workspace files',
        },
      }),
  }),
}));

vi.mock('../navigation', () => ({
  AppLink: ({ href, children, ...props }: ComponentProps<'a'> & { href: string }) => (
    <a href={href} {...props}>{children}</a>
  ),
}));

const file: WorkspaceFile = {
  id: 'att-1',
  workspaceId: 'ws-1',
  issueId: 'issue-1',
  commentId: null,
  chatSessionId: null,
  chatMessageId: null,
  uploaderType: 'member',
  uploaderId: 'user-1',
  filename: 'roadmap.pdf',
  url: 'https://cdn.example/roadmap.pdf',
  downloadUrl: '/api/attachments/att-1/download',
  markdownUrl: '/api/attachments/att-1/download',
  contentType: 'application/pdf',
  sizeBytes: 2048,
  createdAt: '2026-08-18T20:00:00Z',
  sourceType: 'issue',
  sourceId: 'issue-1',
  sourceTitle: 'Plan launch',
};

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceFilesPage />
    </QueryClientProvider>,
  );
}

describe('WorkspaceFilesPage', () => {
  beforeEach(() => {
    tryOpen.mockClear();
    download.mockClear();
    shouldFail = false;
    response = { attachments: [file], hasMore: false, nextOffset: null };
  });

  it('lists the workspace file with its Issue source and reuses preview and download actions', async () => {
    renderPage();

    expect(await screen.findByText('roadmap.pdf')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Issue.*Plan launch/ })).toHaveAttribute(
      'href',
      '/studio/issues/issue-1',
    );

    fireEvent.click(screen.getByRole('button', { name: 'Preview roadmap.pdf' }));
    expect(tryOpen).toHaveBeenCalledWith({
      kind: 'full',
      attachment: expect.objectContaining({
        id: 'att-1',
        workspace_id: 'ws-1',
        issue_id: 'issue-1',
        content_type: 'application/pdf',
        size_bytes: 2048,
      }),
    });

    fireEvent.click(screen.getByRole('button', { name: 'Download roadmap.pdf' }));
    expect(download).toHaveBeenCalledWith('att-1');
  });

  it('renders a clear empty state', async () => {
    response = { attachments: [], hasMore: false, nextOffset: null };
    renderPage();
    expect(await screen.findByText('No shared files yet')).toBeInTheDocument();
  });

  it('shows a loading state while the library request is pending', () => {
    renderPage();
    expect(screen.getByRole('status', { name: 'Loading files' })).toBeInTheDocument();
  });

  it('shows a recoverable error state', async () => {
    shouldFail = true;
    renderPage();
    expect(await screen.findByText('Files could not be loaded.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});
