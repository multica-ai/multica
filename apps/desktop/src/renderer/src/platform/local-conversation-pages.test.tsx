import { act, cleanup, render, waitFor } from "@testing-library/react";
import { ConversationListModel } from "@multica/conversation-client/models";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { observer } from "mobx-react-lite";
import { afterEach, expect, it, vi } from "vitest";
import type { LocalSessionEntry } from "./local-conversation-projects";
import { useLocalConversationPages } from "./local-conversation-pages";

const fixture = vi.hoisted(() => ({ client: null as unknown }));
vi.mock("./conversation-services", () => ({ conversationService: () => fixture.client }));
afterEach(cleanup);

const summary = (id: string, seq = 1, title = id) => localV2SummarySchema.parse({
  conversation_id: id, summary_seq: seq, provider: "codex", title, status: "idle", pinned: false,
  archived: false, turn_count: 1, updated_at: "2026-11-04T00:00:00Z",
});

it("keeps unrelated row props stable across selection and summary updates", async () => {
  const list = new ConversationListModel();
  list.upsert([summary("a"), summary("b"), summary("c")]);
  fixture.client = { list, loadSummaries: vi.fn(async () => {}) };
  const renders = new Map<string, number>();
  const Row = observer(({ entry, selected }: { entry: LocalSessionEntry; selected: boolean }) => {
    renders.set(entry.id, (renders.get(entry.id) ?? 0) + 1);
    return <div aria-current={selected}>{entry.title}</div>;
  });
  const transport = {
    availability: () => ({ state: "connected" }),
    onAvailability: () => () => {}, onConnection: () => () => {},
  } as unknown as Parameters<typeof useLocalConversationPages>[0];
  const scope = { userId: "user", workspaceId: "workspace" };
  const projects: Parameters<typeof useLocalConversationPages>[2] = [];
  const hidden: string[] = [];
  let loaded = false;
  let pageRenders = 0;
  const Page = observer(({ selected }: { selected: string }) => {
    pageRenders++;
    const page = useLocalConversationPages(transport, scope, projects, hidden, false, "", true);
    loaded = !page.isPending;
    return <>{page.data.map((entry) => <Row key={entry.id} entry={entry} selected={selected === entry.id} />)}</>;
  });
  const app = render(<Page selected="a" />);
  await waitFor(() => expect(loaded).toBe(true));
  renders.clear();
  app.rerender(<Page selected="b" />);
  expect(Object.fromEntries(renders)).toEqual({ a: 1, b: 1 });
  renders.clear();
  act(() => list.upsert([summary("a", 2, "Renamed")]));
  expect(Object.fromEntries(renders)).toEqual({ a: 1 });
  renders.clear();
  const before = pageRenders;
  act(() => list.upsert([summary("a", 3, "Renamed")]));
  expect(pageRenders).toBe(before);
  expect(renders.size).toBe(0);
});
