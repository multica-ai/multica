import { describe, expect, it } from "vitest";
import { buildUnifiedSearchResults } from "./unified-search";

describe("buildUnifiedSearchResults", () => {
  it("ranks mixed result types with hard exact boosts, RRF, recency tiebreak, and filters", () => {
    const result = buildUnifiedSearchResults(
      {
        issues: [
          {
            id: "issue-1",
            identifier: "FIR-1234",
            title: "Frontend build hangs",
            updated_at: "2026-06-23T10:00:00Z",
          },
          {
            id: "issue-2",
            identifier: "FIR-9",
            title: "Kubernetes migration",
            updated_at: "2026-06-25T08:00:00Z",
          },
        ],
        notes: [
          {
            id: "note-1",
            title: "Frontend build hangs",
            updated_at: "2026-06-20T08:00:00Z",
          },
          {
            id: "note-2",
            title: "Ranked search design",
            updated_at: "2026-06-25T09:00:00Z",
          },
        ],
        projects: [
          {
            id: "project-1",
            title: "Search",
            updated_at: "2026-06-25T07:00:00Z",
          },
        ],
        chats: [
          {
            id: "chat-1",
            title: "Search planning",
            updated_at: "2026-06-25T09:30:00Z",
          },
        ],
        messages: [
          {
            id: "message-1",
            title: "Search planning",
            updated_at: "2026-06-25T09:45:00Z",
          },
        ],
      },
      "FIR-1234",
    );

    expect(result.counts).toEqual({
      all: 7,
      issues: 2,
      notes: 2,
      projects: 1,
      chats: 1,
      messages: 1,
    });
    expect(result.all.map((item) => `${item.type}:${item.id}`)).toEqual([
      "issues:issue-1",
      "messages:message-1",
      "chats:chat-1",
      "projects:project-1",
      "notes:note-1",
      "notes:note-2",
      "issues:issue-2",
    ]);
    expect(result.byFilter.notes.map((item) => item.id)).toEqual(["note-1", "note-2"]);
  });
});
