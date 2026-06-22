import { describe, it, expect } from "vitest";
import type { Channel, InboxItem } from "@multica/core/types";
import { buildChannelThreadEntries } from "./thread-entries";

// Mirrors the exact /api/inbox shape observed on prod for FIR-1854: a DM reply
// whose details carry thread_root_id, plus the duplicate `mentioned` item the
// same reply generates when it @-mentions the recipient.
function inboxItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    id: over.id ?? "item",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "agent",
    actor_id: "a1",
    type: "dm_message",
    severity: "info",
    route: "inbox",
    issue_id: "ad2534eb",
    project_id: null,
    title: "t",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-06-22T16:52:58Z",
    details: null,
    ...over,
  } as InboxItem;
}

function channel(over: Partial<Channel> = {}): Channel {
  return { id: "ad2534eb", kind: "dm", unread_count: 4, project_id: null, ...over } as Channel;
}

describe("buildChannelThreadEntries (FIR-1854)", () => {
  it("splits a DM thread reply into its own row (prod repro: Filip DM)", () => {
    const dm = channel({ id: "ad2534eb", kind: "dm" });
    const channelMap = new Map([[dm.id, dm]]);
    // The fresh DM reply, exactly as prod returns it post-deploy.
    const reply = inboxItem({
      id: "f9699b3a",
      type: "dm_message",
      issue_id: "ad2534eb",
      details: {
        comment_id: "c0615376-251c-4af7-98b5-cacd217df85c",
        thread_root_id: "9090608d-1907-4291-8c5c-d5ad35089ba8",
        thread_root_preview: "FRESH thread-split re-test (FIR-1854)",
      },
    });
    const out = buildChannelThreadEntries([reply], channelMap);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({
      kind: "thread",
      channelKind: "dm",
      channelId: "ad2534eb",
      threadRootId: "9090608d-1907-4291-8c5c-d5ad35089ba8",
    });
  });

  it("splits a channel thread reply too (parity)", () => {
    const ch = channel({ id: "chan1", kind: "channel" });
    const channelMap = new Map([[ch.id, ch]]);
    const reply = inboxItem({
      id: "r1",
      type: "new_comment",
      issue_id: "chan1",
      details: { comment_id: "c1", thread_root_id: "root1", thread_root_preview: "p" },
    });
    const out = buildChannelThreadEntries([reply], channelMap);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({ kind: "thread", channelKind: "channel" });
  });

  it("does not split when the thread has no unread reply", () => {
    const dm = channel({ id: "ad2534eb", kind: "dm" });
    const channelMap = new Map([[dm.id, dm]]);
    const read = inboxItem({
      id: "f9699b3a",
      read: true,
      details: { thread_root_id: "9090608d", comment_id: "c0615376" },
    });
    expect(buildChannelThreadEntries([read], channelMap)).toHaveLength(0);
  });
});
