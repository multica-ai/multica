import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ReactNode } from "react";
import type { InboxItem } from "@multica/core/types";
import enInbox from "../../locales/en/inbox.json";

const mockOpenModal = vi.hoisted(() => vi.fn());
const mockSetDraft = vi.hoisted(() => vi.fn());
const mockReplace = vi.hoisted(() => vi.fn());
const mockListInbox = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/ws-test/inbox",
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
  }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: mockOpenModal }),
  },
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: {
    getState: () => ({ setDraft: mockSetDraft }),
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listInbox: mockListInbox,
    markInboxRead: vi.fn().mockResolvedValue(undefined),
    archiveInbox: vi.fn().mockResolvedValue(undefined),
    markAllInboxRead: vi.fn().mockResolvedValue(undefined),
    archiveAllInbox: vi.fn().mockResolvedValue(undefined),
    archiveAllReadInbox: vi.fn().mockResolvedValue(undefined),
    archiveCompletedInbox: vi.fn().mockResolvedValue(undefined),
  },
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    searchParams: new URLSearchParams(),
    replace: mockReplace,
  }),
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({
    defaultLayout: undefined,
    onLayoutChanged: vi.fn(),
  }),
}));

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ResizablePanel: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ResizableHandle: () => null,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ children, render }: { children?: ReactNode; render?: ReactNode }) => <>{render ?? children}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  DropdownMenuSeparator: () => null,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
    disabled,
    type = "button",
  }: {
    children?: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: () => <div data-testid="skeleton" />,
}));

vi.mock("@multica/ui/components/common/error-boundary", () => ({
  ErrorBoundary: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("../../issues/components", () => ({
  IssueDetail: () => <div data-testid="issue-detail" />,
  StatusIcon: () => <span data-testid="status-icon" />,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../layout/page-header", () => ({
  PageHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

import { InboxPage } from "./inbox-page";

function inboxItem(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "ws-test",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "quick_create_failed",
    severity: "action_required",
    issue_id: null,
    title: "Quick create failed",
    body: "Missing environment variable: `OPENAI_API_KEY`.",
    issue_status: null,
    read: true,
    archived: false,
    created_at: "2026-05-15T02:04:55Z",
    details: {
      agent_id: "devops-agent-id",
      original_prompt: "平台升级cli失败",
      error: "Missing environment variable: `OPENAI_API_KEY`.",
    },
    ...overrides,
  };
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nProvider locale="en" resources={{ en: { inbox: enInbox } }}>
      <QueryClientProvider client={qc}>
        <InboxPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("InboxPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListInbox.mockResolvedValue([inboxItem({})]);
  });

  it("opens the advanced form without carrying the failed quick-create agent as assignee", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByText("平台升级cli失败"));
    await screen.findByRole("heading", { name: "平台升级cli失败" });
    await user.click(screen.getByRole("button", { name: /Edit as advanced form/i }));

    expect(mockSetDraft).toHaveBeenCalledWith({
      description: "平台升级cli失败",
      assigneeType: undefined,
      assigneeId: undefined,
    });
    expect(mockOpenModal).toHaveBeenCalledWith("create-issue");
  });
});
