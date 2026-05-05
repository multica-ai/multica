import { describe, it, expect, vi } from "vitest";
import { buildAnchorMarkdown } from "./context-anchor";

// Minimal mock t function that returns the English dict values
const t = vi.fn((key: string, params?: Record<string, string | number>) => {
  const dict: Record<string, string> = {
    anchor_context_prefix: "Context: ",
    anchor_context_project: `Context: Project "${params?.label ?? ""}"`,
  };
  return dict[key] ?? key;
});

describe("buildAnchorMarkdown", () => {
  it("formats an issue anchor as a mention link with title subtitle", () => {
    const md = buildAnchorMarkdown(
      {
        type: "issue",
        id: "uuid-123",
        label: "MUL-42",
        subtitle: "Fix login redirect",
      },
      t,
    );
    expect(md).toBe(
      'Context: [MUL-42](mention://issue/uuid-123) — "Fix login redirect"',
    );
  });

  it("omits the subtitle clause when none is provided", () => {
    const md = buildAnchorMarkdown(
      {
        type: "issue",
        id: "uuid-x",
        label: "MUL-7",
      },
      t,
    );
    expect(md).toBe("Context: [MUL-7](mention://issue/uuid-x)");
  });

  it("formats a project anchor as plain text (no mention type)", () => {
    const md = buildAnchorMarkdown(
      {
        type: "project",
        id: "proj-uuid",
        label: "Authentication",
      },
      t,
    );
    expect(md).toBe('Context: Project "Authentication"');
  });
});
