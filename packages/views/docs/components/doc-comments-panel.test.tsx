import { forwardRef, useImperativeHandle, useState } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { DocComment } from "@multica/core/types";
import { DocCommentsPanel, groupDocThreads } from "./doc-comments-panel";
import enDocs from "../../locales/en/docs.json";

const { createMock, resolveMock, removeMock } = vi.hoisted(() => ({
  createMock: vi.fn(),
  resolveMock: vi.fn(),
  removeMock: vi.fn(),
}));

vi.mock("@multica/core/docs", () => ({
  docTaskRunsOptions: (_ws: string, docId: string) => ({ queryKey: ["doc-runs", docId], queryFn: async () => [] }),
  useCreateDocComment: () => ({ mutateAsync: createMock, isPending: false }),
  useSetDocCommentResolved: () => ({ mutateAsync: resolveMock, isPending: false }),
  useDeleteDocComment: () => ({ mutateAsync: removeMock, isPending: false }),
}));
vi.mock("@multica/core/hooks", async (original) => ({
  ...await original<object>(), useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "me" } }),
}));
vi.mock("@multica/core/appearance", () => ({ useReducedMotion: () => true }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "me" ? "Jiayuan" : "Lambda") }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/shortcut-keycaps", () => ({ ActionShortcutKeycaps: () => null }));
vi.mock("../../issues/components/composer-frame", () => ({ QUIET_SEND_CLASS: "" }));
vi.mock("./doc-thread-runs", () => ({ docThreadRuns: () => [], DocThreadRun: () => null }));
vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  ContentEditor: forwardRef<unknown, { placeholder: string; onUpdate: (md: string) => void }>(
    function ContentEditor({ placeholder, onUpdate }, ref) {
      const [value, setValue] = useState("");
      useImperativeHandle(ref, () => ({
        getMarkdown: () => value,
        clearContent: () => setValue(""),
        focus: () => {},
      }));
      return (
        <textarea
          aria-label={placeholder}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            onUpdate(e.target.value);
          }}
        />
      );
    },
  ),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const QUOTE = "Migrations must rehearse";

function comment(id: string, extra: Partial<DocComment> = {}): DocComment {
  return {
    id, docId: "d1", parentId: null, authorType: "member", authorId: "me", content: `body of ${id}`,
    anchorQuote: QUOTE, anchorPrefix: "", anchorSuffix: "",
    resolvedAt: null, resolvedByType: null, resolvedById: null,
    createdAt: "2026-11-25T09:00:00Z", updatedAt: "2026-11-25T09:00:00Z", ...extra,
  };
}

const discussion = (rootExtra: Partial<DocComment> = {}) =>
  groupDocThreads([
    comment("c1", rootExtra),
    comment("c2", { parentId: "c1", authorType: "agent", authorId: "agent-1" }),
  ]);

function mount(props: Partial<Parameters<typeof DocCommentsPanel>[0]> = {}) {
  const handlers = { onActivate: vi.fn(), onDeactivate: vi.fn(), onDraftDone: vi.fn(), onClose: vi.fn() };
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { docs: enDocs } }}>
        <DocCommentsPanel
          docId="d1"
          threads={discussion()}
          missing={new Set()}
          activeId={null}
          draft={null}
          {...handlers}
          {...props}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return handlers;
}

it("folds a discussion that is not selected to its first message and a reply count", () => {
  const { onActivate } = mount();
  const thread = screen.getByRole("group", { name: QUOTE });
  expect(thread.textContent).toContain("body of c1");
  expect(thread.textContent).toContain("1 reply");
  expect(screen.queryByText("body of c2")).toBeNull();
  expect(screen.queryByRole("textbox", { name: "Reply, or @ someone..." })).toBeNull();

  fireEvent.click(thread);
  expect(onActivate).toHaveBeenCalledWith("c1");
});

it("opens a folded discussion from the keyboard", () => {
  const { onActivate } = mount();
  const thread = screen.getByRole("group", { name: QUOTE });
  expect(thread.tabIndex).toBe(0);
  fireEvent.keyDown(thread, { key: "Enter" });
  expect(onActivate).toHaveBeenCalledWith("c1");
});

it("unfolds the selected discussion with its replies and a reply box that sends from inside it", async () => {
  createMock.mockResolvedValue(comment("c3"));
  mount({ activeId: "c1" });
  expect(screen.getByText("body of c2")).toBeTruthy();
  expect(screen.queryByText("1 reply")).toBeNull();

  const send = screen.getByRole("button", { name: "Reply" });
  expect((send as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByRole("textbox", { name: "Reply, or @ someone..." }), { target: { value: "On it" } });
  expect((send as HTMLButtonElement).disabled).toBe(false);
  fireEvent.click(send);
  await waitFor(() => expect(createMock).toHaveBeenCalledWith({ content: "On it", parentId: "c1" }));
});

it("resolves the selected discussion from its header and lets go of it", async () => {
  resolveMock.mockResolvedValue({});
  const { onDeactivate } = mount({ activeId: "c1" });
  fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
  await waitFor(() => expect(resolveMock).toHaveBeenCalledWith({ commentId: "c1", resolved: true }));
  await waitFor(() => expect(onDeactivate).toHaveBeenCalled());
});

it("marks a discussion whose text was edited away on the quote line", () => {
  mount({ missing: new Set(["c1"]) });
  const tag = screen.getByText("Text changed");
  expect(tag.getAttribute("title")).toBe("The commented text was changed");
});

it("deletes only the viewer's own messages", async () => {
  removeMock.mockResolvedValue({});
  mount({ activeId: "c1" });
  const buttons = screen.getAllByRole("button", { name: "Delete comment" });
  expect(buttons).toHaveLength(1);
  fireEvent.click(buttons[0]!);
  await waitFor(() => expect(removeMock).toHaveBeenCalledWith("c1"));
});

it("reopens a resolved discussion from its header", async () => {
  resolveMock.mockResolvedValue({});
  const { onActivate } = mount({
    threads: discussion({ resolvedAt: "2026-11-25T10:00:00Z", resolvedByType: "member", resolvedById: "me" }),
  });
  fireEvent.click(screen.getByRole("tab", { name: "Resolved 1" }));
  expect(screen.getByText(/Resolved by Jiayuan/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Reopen" }));
  await waitFor(() => expect(resolveMock).toHaveBeenCalledWith({ commentId: "c1", resolved: false }));
  await waitFor(() => expect(onActivate).toHaveBeenCalledWith("c1"));
});
