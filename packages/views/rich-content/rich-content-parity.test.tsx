/**
 * Five-surface RichContent parity (MUL-4922).
 *
 * The acceptance naiyuan set: ONE completed Markdown fixture must produce the
 * SAME semantic blocks in Chat (user message, live assistant, persisted
 * assistant), an Issue description and a Comment. Density/CSS may differ;
 * capability may not. A Mermaid fence must be a diagram in all five, not a code
 * block in some of them.
 *
 * The surfaces are exercised through their real entry points — ReadonlyContent
 * for Issue/Comment and ChatMessageList for the three Chat rows — so a
 * regression that reintroduces a Chat-only renderer fails here.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";

const { resolveIssueIdentifierMock, mermaidRenderMock } = vi.hoisted(() => ({
  resolveIssueIdentifierMock: vi.fn(),
  mermaidRenderMock: vi.fn(),
}));

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: (identifier: string) =>
    resolveIssueIdentifierMock(identifier),
}));

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  const chat = (await import("../locales/en/chat.json")).default;
  return {
    useT: (ns?: string) => ({
      t: (select: (bundle: Record<string, unknown>) => string) =>
        select((ns === "chat" ? chat : editor) as Record<string, unknown>),
    }),
    useTimeAgo: () => "just now",
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
  useWorkspaceSlug: () => "test",
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  useAppOrigin: () => null,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span data-testid="issue-mention">{fallbackLabel ?? issueId}</span>
  ),
}));

vi.mock("../projects/components/project-chip", () => ({
  ProjectChip: ({ projectId }: { projectId: string }) => (
    <span data-testid="project-chip">{projectId}</span>
  ),
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: mermaidRenderMock,
  },
}));

// react-virtuoso does not virtualize under jsdom's zero-height viewport, so
// render every row directly. computeItemKey is still exercised: it is what
// gives the live row and the persisted row one identity.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
    components,
    context,
  }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    components?: { Footer?: (p: { context?: unknown }) => ReactElement | null };
    context?: unknown;
  }) => {
    const Footer = components?.Footer;
    return (
      <div>
        {data.map((item, i) => (
          <div key={computeItemKey(i, item)} data-row-key={computeItemKey(i, item)}>
            {itemContent(i, item)}
          </div>
        ))}
        {Footer ? <Footer context={context} /> : null}
      </div>
    );
  },
}));

Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  value: () => ({
    fillStyle: "#000",
    fillRect: vi.fn(),
    getImageData: () => ({ data: new Uint8ClampedArray([12, 34, 56, 255]) }),
  }),
});

import { ReadonlyContent } from "../editor/readonly-content";
import { ChatMessageList } from "../chat/components/chat-message-list";
import { taskMessagesOptions } from "@multica/core/chat/queries";

// naiyuan's fixture, corrected to a single closed fence.
const MERMAID_FIXTURE = [
  "```mermaid",
  "flowchart LR",
  '    HTML["HTML"] --> WEB["网页"]',
  '    CSS["CSS"] --> WEB',
  "```",
].join("\n");

const TASK_ID = "11111111-1111-4111-8111-111111111111";

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
}

function withClient(ui: ReactElement, client: QueryClient) {
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
}

/** A persisted assistant chat_message carrying `content`. */
function assistantMessage(content: string) {
  return {
    id: "msg-assistant-1",
    role: "assistant" as const,
    content,
    task_id: TASK_ID,
    attachments: [],
    elapsed_ms: 1200,
  };
}

function userMessage(content: string) {
  return {
    id: "msg-user-1",
    role: "user" as const,
    content,
    task_id: null,
    attachments: [],
  };
}

/** Seed the task-messages cache the way useRealtimeSync does during a run. */
function seedTimeline(client: QueryClient, text: string) {
  client.setQueryData(taskMessagesOptions(TASK_ID).queryKey, [
    {
      task_id: TASK_ID,
      issue_id: null,
      seq: 1,
      type: "text",
      content: text,
    },
  ] as never);
}

beforeEach(() => {
  vi.clearAllMocks();
  mermaidRenderMock.mockResolvedValue({
    svg: '<svg viewBox="0 0 123 45"><g><text>diagram</text></g></svg>',
  });
  resolveIssueIdentifierMock.mockReturnValue(null);
});

/**
 * The Mermaid leaf's own container — NOT a bare `svg` query, which also matches
 * the lucide icons in code-block chrome.
 */
const mermaidLeaf = (container: HTMLElement): Element | null =>
  container.querySelector(".mermaid-diagram");

/** Every surface must expose the diagram through the same Mermaid leaf. */
async function expectMermaidRendered(container: HTMLElement) {
  await waitFor(() => {
    expect(mermaidLeaf(container)).not.toBeNull();
  });
  await waitFor(() => {
    expect(mermaidLeaf(container)?.querySelector("svg")).not.toBeNull();
  });
  // Not a plain code block: the fence was upgraded.
  expect(container.querySelector("code.hljs")).toBeNull();
}

describe("Mermaid parity across the five surfaces", () => {
  it("renders a diagram in an Issue description", async () => {
    const { container } = render(<ReadonlyContent content={MERMAID_FIXTURE} />);
    await expectMermaidRendered(container);
  });

  it("renders a diagram in a Comment", async () => {
    // Comment and Issue description share ReadonlyContent; asserting both keeps
    // the acceptance list honest about what was actually exercised.
    const { container } = render(
      <ReadonlyContent content={MERMAID_FIXTURE} attachments={[]} />,
    );
    await expectMermaidRendered(container);
  });

  it("renders a diagram in a Chat user message", async () => {
    const client = makeClient();
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[userMessage(MERMAID_FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    await expectMermaidRendered(container);
  });

  it("renders a diagram in a persisted Chat assistant message", async () => {
    const client = makeClient();
    seedTimeline(client, MERMAID_FIXTURE);
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[assistantMessage(MERMAID_FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    await expectMermaidRendered(container);
  });

  it("renders a diagram in a live (streaming) Chat assistant row", async () => {
    const client = makeClient();
    seedTimeline(client, MERMAID_FIXTURE);
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: "running" } as never}
          availability={undefined}
        />,
        client,
      ),
    );
    await expectMermaidRendered(container);
  });
});

describe("streaming fence gate in Chat", () => {
  it("does not instantiate Mermaid while the fence is still open", async () => {
    const client = makeClient();
    // Same content, closing fence not yet streamed.
    seedTimeline(client, "```mermaid\nflowchart LR\n  A --> B\n");
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: "running" } as never}
          availability={undefined}
        />,
        client,
      ),
    );

    // Source is shown as ordinary code; Mermaid is never called.
    await waitFor(() => {
      expect(container.querySelector("code")).not.toBeNull();
    });
    expect(mermaidRenderMock).not.toHaveBeenCalled();
    expect(mermaidLeaf(container)).toBeNull();
  });

  it("upgrades in place once the closing fence arrives", async () => {
    const client = makeClient();
    seedTimeline(client, "```mermaid\nflowchart LR\n  A --> B\n");
    const { container, rerender } = render(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: "running" } as never}
          availability={undefined}
        />,
        client,
      ),
    );
    expect(mermaidRenderMock).not.toHaveBeenCalled();

    // Closing fence streams in.
    seedTimeline(client, "```mermaid\nflowchart LR\n  A --> B\n```");
    rerender(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: "running" } as never}
          availability={undefined}
        />,
        client,
      ),
    );

    await waitFor(() => {
      expect(mermaidLeaf(container)).not.toBeNull();
    });
  });

  it("keeps a settled-but-unclosed fence as source", async () => {
    // Task finished, fence still malformed: completion must not bypass the gate.
    const client = makeClient();
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[assistantMessage("```mermaid\nflowchart LR\n  A --> B\n")] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    await waitFor(() => {
      expect(container.querySelector("code")).not.toBeNull();
    });
    expect(mermaidRenderMock).not.toHaveBeenCalled();
  });
});

describe("live → persisted row identity", () => {
  // Howard's contract #1: the handoff must be an in-place data swap, not a
  // remount, or every completed task rebuilds its diagrams and the scroll
  // position jumps.
  it("keeps one row key across the handoff and does not re-run Mermaid", async () => {
    const client = makeClient();
    seedTimeline(client, MERMAID_FIXTURE);

    const live = (
      <ChatMessageList
        messages={[]}
        pendingTask={{ task_id: TASK_ID, status: "running" } as never}
        availability={undefined}
      />
    );
    const { container, rerender } = render(withClient(live, client));

    await waitFor(() => expect(mermaidLeaf(container)).not.toBeNull());
    const liveKey = container.querySelector("[data-row-key]")?.getAttribute("data-row-key");
    const rendersWhileLive = mermaidRenderMock.mock.calls.length;
    expect(liveKey).toBe(`task:${TASK_ID}`);

    // The assistant message persists; the pending task clears.
    rerender(
      withClient(
        <ChatMessageList
          messages={[assistantMessage(MERMAID_FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );

    await waitFor(() => {
      expect(screen.getByText(/Replied in/i)).toBeInTheDocument();
    });

    // Same key => React reconciled the row instead of remounting it.
    const persistedKey = container
      .querySelector("[data-row-key]")
      ?.getAttribute("data-row-key");
    expect(persistedKey).toBe(liveKey);

    // And the diagram was not re-rendered by the handoff.
    expect(mermaidRenderMock.mock.calls.length).toBe(rendersWhileLive);
    expect(mermaidLeaf(container)).not.toBeNull();
  });

  it("gives a persisted assistant message a task-scoped row key", () => {
    const client = makeClient();
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[assistantMessage("hi")] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    expect(container.querySelector("[data-row-key]")?.getAttribute("data-row-key")).toBe(
      `task:${TASK_ID}`,
    );
  });
});

describe("semantic parity beyond Mermaid", () => {
  const FIXTURE = [
    "A [link](https://example.com) and a mention [MUL-7](mention://issue/MUL-7).",
    "",
    "```html",
    "<b>preview</b>",
    "```",
    "",
    "```ts",
    "const a = 1;",
    "```",
    "",
    "<mark>highlighted</mark>",
  ].join("\n");

  function renderReadonly() {
    return render(<ReadonlyContent content={FIXTURE} />).container;
  }

  function renderChatUser() {
    const client = makeClient();
    return render(
      withClient(
        <ChatMessageList
          messages={[userMessage(FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    ).container;
  }

  it("produces the same block set in Issue/Comment and Chat", async () => {
    resolveIssueIdentifierMock.mockImplementation((id: string) =>
      id === "MUL-7" ? { id: "issue-7", identifier: "MUL-7" } : null,
    );

    const readonly = renderReadonly();
    const chat = renderChatUser();

    for (const container of [readonly, chat]) {
      // HTML fence -> sandboxed preview iframe (not a code block)
      await waitFor(() => {
        expect(container.querySelector("iframe")).not.toBeNull();
      });
      expect(container.querySelector("iframe")?.getAttribute("sandbox")).toBe(
        "allow-scripts",
      );
      // Ordinary language -> lowlight static code
      expect(container.querySelector("code.hljs")).not.toBeNull();
      // Mention chip
      expect(within(container).getByTestId("issue-mention")).toBeInTheDocument();
      // Highlight
      expect(container.querySelector("mark")?.textContent).toBe("highlighted");
      // Plain link
      expect(container.querySelector('a[href="https://example.com"]')).not.toBeNull();
    }
  });

  it("does not dispatch htmlbars / mermaidx to rich blocks on either surface", async () => {
    const near = "```htmlbars\n<b>x</b>\n```\n\n```mermaidx\ngraph TD\n```";
    const readonly = render(<ReadonlyContent content={near} />).container;
    const client = makeClient();
    const chat = render(
      withClient(
        <ChatMessageList
          messages={[userMessage(near)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    ).container;

    for (const container of [readonly, chat]) {
      expect(container.querySelector("iframe")).toBeNull();
      expect(container.querySelectorAll("code.hljs").length).toBe(2);
    }
    expect(mermaidRenderMock).not.toHaveBeenCalled();
  });
});
