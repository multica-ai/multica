import { describe, it, expect } from "vitest";
import {
  DEFAULT_PLACEMENT,
  readPlacement,
  hiddenFromInbox,
  conversationKindOfChannel,
  canTurnOff,
  setPlace,
  showsInChat,
  showsInInbox,
  type ChatPlacement,
} from "./chat-placement";

const BOTH: ChatPlacement = {
  channel: { chat: true, inbox: true },
  dm: { chat: true, inbox: true },
  agent_chat: { chat: true, inbox: true },
};

describe("readPlacement", () => {
  it("falls back to today's behaviour for an absent blob", () => {
    expect(readPlacement(undefined)).toEqual(DEFAULT_PLACEMENT);
    expect(readPlacement(null)).toEqual(DEFAULT_PLACEMENT);
  });

  it("falls back for a malformed blob", () => {
    expect(readPlacement("chat")).toEqual(DEFAULT_PLACEMENT);
    expect(readPlacement(42)).toEqual(DEFAULT_PLACEMENT);
    expect(readPlacement([])).toEqual(DEFAULT_PLACEMENT);
  });

  it("keeps the parts it can read and defaults the rest", () => {
    const parsed = readPlacement({ dm: { chat: true, inbox: false } });
    expect(parsed.dm).toEqual({ chat: true, inbox: false });
    expect(parsed.channel).toEqual(DEFAULT_PLACEMENT.channel);
    expect(parsed.agent_chat).toEqual(DEFAULT_PLACEMENT.agent_chat);
  });

  it("repairs a stored entry that is off in both places", () => {
    const parsed = readPlacement({ channel: { chat: false, inbox: false } });
    expect(parsed.channel).toEqual(DEFAULT_PLACEMENT.channel);
  });

  it("treats non-boolean truthiness as off", () => {
    const parsed = readPlacement({ dm: { chat: "yes", inbox: 1 } });
    expect(parsed.dm).toEqual(DEFAULT_PLACEMENT.dm);
  });
});

describe("showsInChat / showsInInbox", () => {
  it("reads the default as chat-only", () => {
    // FIR-4350 — turning the feature on moves conversations to Chat and out of
    // the Inbox; that is the default.
    expect(showsInChat(DEFAULT_PLACEMENT, "channel")).toBe(true);
    expect(showsInInbox(DEFAULT_PLACEMENT, "channel")).toBe(false);
  });

  it("reads both places when both are on", () => {
    expect(showsInChat(BOTH, "agent_chat")).toBe(true);
    expect(showsInInbox(BOTH, "agent_chat")).toBe(true);
  });
});

describe("hiddenFromInbox", () => {
  it("hides every conversation type by default (all placed in Chat)", () => {
    const hidden = hiddenFromInbox(DEFAULT_PLACEMENT);
    expect(hidden.size).toBe(3);
    expect(hidden.has("channel")).toBe(true);
    expect(hidden.has("dm")).toBe(true);
    expect(hidden.has("agent_chat")).toBe(true);
  });

  it("hides exactly the types whose inbox place is off", () => {
    const placement = setPlace(
      setPlace(BOTH, "dm", "inbox", false),
      "channel",
      "inbox",
      false,
    );
    const hidden = hiddenFromInbox(placement);
    expect(hidden.has("dm")).toBe(true);
    expect(hidden.has("channel")).toBe(true);
    expect(hidden.has("agent_chat")).toBe(false);
  });
});

describe("conversationKindOfChannel", () => {
  it("maps dm and group onto dm", () => {
    expect(conversationKindOfChannel("dm")).toBe("dm");
    expect(conversationKindOfChannel("group")).toBe("dm");
  });

  it("maps channel and anything unknown onto channel", () => {
    expect(conversationKindOfChannel("channel")).toBe("channel");
    expect(conversationKindOfChannel(undefined)).toBe("channel");
    expect(conversationKindOfChannel("something_new")).toBe("channel");
  });
});

describe("canTurnOff / setPlace — a type can never be placed nowhere", () => {
  it("refuses to turn off the last remaining place", () => {
    // The default places every type in Chat only, so Chat is the last place.
    expect(canTurnOff(DEFAULT_PLACEMENT, "channel", "chat")).toBe(false);
    expect(setPlace(DEFAULT_PLACEMENT, "channel", "chat", false)).toBe(DEFAULT_PLACEMENT);
  });

  it("allows turning off a place while the other stays on", () => {
    expect(canTurnOff(BOTH, "channel", "inbox")).toBe(true);
    expect(setPlace(BOTH, "channel", "inbox", false).channel).toEqual({
      chat: true,
      inbox: false,
    });
  });

  it("allows turning a place back on", () => {
    const chatOnly = setPlace(BOTH, "dm", "inbox", false);
    expect(setPlace(chatOnly, "dm", "inbox", true).dm).toEqual({ chat: true, inbox: true });
  });

  it("treats an already-off place as turnable off (no-op)", () => {
    // Inbox is off in the default, so "turning it off" is a no-op.
    expect(canTurnOff(DEFAULT_PLACEMENT, "channel", "inbox")).toBe(true);
  });

  it("leaves the other types untouched", () => {
    const next = setPlace(BOTH, "dm", "inbox", false);
    expect(next.channel).toEqual(BOTH.channel);
    expect(next.agent_chat).toEqual(BOTH.agent_chat);
  });
});
