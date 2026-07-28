import { describe, expect, it } from "vitest";

import {
  clampCaret,
  liveCollabUrl,
  otherPeers,
  presenceLabel,
  pruneCarets,
  upsertCaret,
  type LivePeer,
  type RemoteCaret,
} from "./note-live-collab-protocol";

const peer = (id: string, name: string, userId = `u-${id}`): LivePeer => ({
  id,
  user_id: userId,
  name,
  color: "#2563eb",
  can_edit: true,
});

const caret = (peerId: string, head = 5): RemoteCaret => ({
  peerId,
  name: "Alice",
  color: "#2563eb",
  anchor: head,
  head,
});

describe("liveCollabUrl", () => {
  it("upgrades https to a secure websocket", () => {
    expect(liveCollabUrl("https://multica.firtal.com", "note-1")).toBe(
      "wss://multica.firtal.com/api/cerebro/notes/note-1/collab",
    );
  });

  it("keeps plain http on a local dev server", () => {
    expect(liveCollabUrl("http://localhost:8080", "note-1")).toBe(
      "ws://localhost:8080/api/cerebro/notes/note-1/collab",
    );
  });

  it("does not double the slash when the base url ends with one", () => {
    expect(liveCollabUrl("https://multica.firtal.com/", "note-1")).toBe(
      "wss://multica.firtal.com/api/cerebro/notes/note-1/collab",
    );
  });
});

describe("otherPeers", () => {
  it("drops my own connection", () => {
    const got = otherPeers([peer("me", "Me"), peer("p2", "Alice")], "me");
    expect(got.map((p) => p.id)).toEqual(["p2"]);
  });

  it("keeps my other tab, because it has its own caret", () => {
    const mine = peer("me", "Me", "u-1");
    const secondTab = peer("p2", "Me", "u-1");
    const got = otherPeers([mine, secondTab], "me");
    expect(got.map((p) => p.id)).toEqual(["p2"]);
  });
});

describe("upsertCaret", () => {
  it("replaces a peer's previous position instead of leaving a trail", () => {
    const carets = upsertCaret([caret("p2", 5)], caret("p2", 12));
    expect(carets).toHaveLength(1);
    expect(carets[0]?.head).toBe(12);
  });

  it("keeps carets from different peers side by side", () => {
    const carets = upsertCaret([caret("p2", 5)], caret("p3", 9));
    expect(carets.map((c) => c.peerId).sort()).toEqual(["p2", "p3"]);
  });
});

describe("pruneCarets", () => {
  it("removes the caret of someone who closed the note", () => {
    const carets = pruneCarets([caret("p2"), caret("p3")], [peer("p2", "A")]);
    expect(carets.map((c) => c.peerId)).toEqual(["p2"]);
  });
});

describe("clampCaret", () => {
  it("keeps a position that is inside the document", () => {
    expect(clampCaret(4, 10)).toBe(4);
  });

  it("pulls a position past the end back to the end", () => {
    // The sender can be one step ahead of us; an unclamped position throws
    // when ProseMirror resolves it.
    expect(clampCaret(99, 10)).toBe(10);
  });

  it("floors a negative or unusable position at zero", () => {
    expect(clampCaret(-3, 10)).toBe(0);
    expect(clampCaret(Number.NaN, 10)).toBe(0);
  });
});

describe("presenceLabel", () => {
  it("says nothing when nobody else is here", () => {
    expect(presenceLabel([])).toBe("");
  });

  it("names one other editor", () => {
    expect(presenceLabel([peer("p2", "Alice")])).toBe(
      "Alice is editing this note",
    );
  });

  it("names both when there are two", () => {
    expect(presenceLabel([peer("p2", "Alice"), peer("p3", "Bob")])).toBe(
      "Alice and Bob are editing this note",
    );
  });

  it("summarises the rest beyond two", () => {
    expect(
      presenceLabel([
        peer("p2", "Alice"),
        peer("p3", "Bob"),
        peer("p4", "Cara"),
      ]),
    ).toBe("Alice and 2 others are editing this note");
  });
});
