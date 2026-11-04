// @vitest-environment node
import { expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { patchChatSessionPages } from "./session-cache";
import { chatKeys } from "./queries";
import type { ChatSession, ChatSessionsPage, ChatSessionSummary } from "../types";

it("patches loaded pages, detail, and cross-page badges together and rolls them back", () => {
  const qc = new QueryClient();
  const row: ChatSession = { id: "demo", workspace_id: "ws", agent_id: "agent", creator_id: "user", title: "Demo", status: "active", has_unread: true, unread_count: 4, created_at: "", updated_at: "" };
  const data = { pages: [{ sessions: [row], has_more: true, next_cursor: "next" } satisfies ChatSessionsPage], pageParams: [null] };
  const summary: ChatSessionSummary = { active_count: 2000, archived_count: 1000, unread_by_session: { demo: 4, unloaded: 2 } };
  qc.setQueryData(chatKeys.sessionPages("ws", "active"), data);
  qc.setQueryData(chatKeys.session("ws", "demo"), row);
  qc.setQueryData(chatKeys.sessionSummary("ws"), summary);
  const rollback = patchChatSessionPages(qc, "ws", "demo", { status: "archived", has_unread: false, unread_count: 0 });
  expect(qc.getQueryData(chatKeys.sessionSummary("ws"))).toEqual({ active_count: 1999, archived_count: 1001, unread_by_session: { unloaded: 2 } });
  expect(qc.getQueryData<ChatSession>(chatKeys.session("ws", "demo"))?.status).toBe("archived");
  expect(qc.getQueryData<typeof data>(chatKeys.sessionPages("ws", "active"))?.pages[0]?.sessions[0]).toMatchObject({ status: "archived", has_unread: false, unread_count: 0 });
  rollback();
  expect(qc.getQueryData(chatKeys.sessionPages("ws", "active"))).toEqual(data);
  expect(qc.getQueryData(chatKeys.sessionSummary("ws"))).toEqual(summary);
  qc.clear();
});
