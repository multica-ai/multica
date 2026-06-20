import { describe, expect, it } from "vitest";

import {
  formatReminderAnchor,
  formatReminderDue,
  formatReminderSource,
  formatReminderWhen,
} from "./format";
import { pickReminderStrings } from "../strings";
import type { Reminder } from "../core/types";

const S = pickReminderStrings("en");
const LOCALE = "en-US";

// Minimal reminder builder — only the fields formatReminderAnchor reads.
function makeReminder(overrides: Partial<Reminder>): Reminder {
  return {
    id: "r1",
    creator_id: "c1",
    recipient_type: "member",
    recipient_id: "m1",
    remind_at: new Date(2026, 5, 20, 9, 0, 0).toISOString(),
    status: "pending",
    text: "Remember this",
    anchor_type: "none",
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

// Fixed "now" so the relative labels are deterministic. 2026-06-19 10:00 local.
const NOW = new Date(2026, 5, 19, 10, 0, 0);

describe("formatReminderWhen", () => {
  it("labels today with the time", () => {
    const at = new Date(2026, 5, 19, 14, 0, 0).toISOString();
    expect(formatReminderWhen(at, S, LOCALE, NOW)).toMatch(/^Today /);
  });

  it("labels tomorrow with the time", () => {
    const at = new Date(2026, 5, 20, 9, 0, 0).toISOString();
    expect(formatReminderWhen(at, S, LOCALE, NOW)).toMatch(/^Tomorrow /);
  });

  it("labels further out with weekday + date", () => {
    const at = new Date(2026, 5, 25, 9, 0, 0).toISOString();
    const label = formatReminderWhen(at, S, LOCALE, NOW);
    expect(label).not.toMatch(/^Today|^Tomorrow/);
    expect(label).toMatch(/Jun/);
  });

  it("returns empty string for an unparseable date", () => {
    expect(formatReminderWhen("not-a-date", S, LOCALE, NOW)).toBe("");
  });
});

describe("formatReminderDue", () => {
  it("prefixes with Due today at", () => {
    const at = new Date(2026, 5, 19, 14, 0, 0).toISOString();
    expect(formatReminderDue(at, S, LOCALE, NOW)).toMatch(/^Due today at /);
  });
});

describe("formatReminderSource", () => {
  it("formats a DM", () => {
    expect(formatReminderSource("dm", "Filip", S)).toBe("DM · Filip");
  });
  it("formats a channel", () => {
    expect(formatReminderSource("channel", "campaigns", S)).toBe("#campaigns");
  });
  it("falls back to the title for a regular issue", () => {
    expect(formatReminderSource("issue", "Supplier", S)).toBe("Supplier");
  });
  it("has a sensible empty-title fallback", () => {
    expect(formatReminderSource("dm", "", S)).toBe("Direct message");
  });
});

describe("formatReminderAnchor", () => {
  it("describes a comment anchor with a go-to action when reachable", () => {
    const a = formatReminderAnchor(
      makeReminder({
        anchor_type: "comment",
        conversation_kind: "dm",
        conversation_title: "Filip",
        conversation_id: "conv1",
      }),
      S,
    );
    expect(a.type).toBe("comment");
    expect(a.label).toBe("DM · Filip");
    expect(a.actionLabel).toBe("Go to message");
  });

  it("drops the action when the source conversation is gone", () => {
    const a = formatReminderAnchor(
      makeReminder({ anchor_type: "comment", conversation_kind: "dm", conversation_title: "Filip" }),
      S,
    );
    expect(a.actionLabel).toBeNull();
  });

  it("labels an issue anchor", () => {
    const a = formatReminderAnchor(
      makeReminder({ anchor_type: "issue", conversation_title: "Login-bug", conversation_id: "i1" }),
      S,
    );
    expect(a.label).toBe("Issue · Login-bug");
    expect(a.actionLabel).toBe("Go to issue");
  });

  it("labels a project anchor", () => {
    const a = formatReminderAnchor(
      makeReminder({ anchor_type: "project", project_title: "Q3", project_id: "p1" }),
      S,
    );
    expect(a.label).toBe("Project · Q3");
    expect(a.actionLabel).toBe("Go to project");
  });

  it("labels a chat-message anchor and opens the chat", () => {
    const a = formatReminderAnchor(
      makeReminder({
        anchor_type: "chat_message",
        chat_session_title: "Research-agent",
        chat_session_id: "s1",
        chat_message_id: "m1",
      }),
      S,
    );
    expect(a.label).toBe("Chat · Research-agent");
    expect(a.actionLabel).toBe("Go to chat");
  });

  it("drops the chat action when the session is gone", () => {
    const a = formatReminderAnchor(makeReminder({ anchor_type: "chat_message" }), S);
    expect(a.actionLabel).toBeNull();
  });

  it("treats a free reminder as anchorless with no action", () => {
    const a = formatReminderAnchor(makeReminder({ anchor_type: "none" }), S);
    expect(a.label).toBe("Free reminder");
    expect(a.actionLabel).toBeNull();
  });
});
