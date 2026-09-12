import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { InboxDetailLabel } from "./inbox-detail-label";

vi.mock("../../issues/components", () => ({
  StatusIcon: () => null,
  PriorityIcon: () => null,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Someone" }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string, params?: Record<string, string>) => {
      const template = accessor({
        labels: {
          created_with_agent: "Created with agent: {{identifier}}",
          failed_with_detail: "Failed: {{detail}}",
        },
        types: {
          quick_create_done: "Quick-create done",
          quick_create_failed: "Quick-create failed",
        },
      });
      if (!params) return template;
      return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => params[key] ?? "");
    },
  }),
}));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "quick_create_done",
    severity: "info",
    issue_id: null,
    title: "Quick create completed",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-09-12T08:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("InboxDetailLabel quick-create success", () => {
  it("shows the returned draft for a completed quick-create without an issue", () => {
    const draft = "### Proposed issue\n\nThe sync job should expose a retry button.";
    const { container } = render(
      <InboxDetailLabel
        item={item({
          body: draft,
          details: { original_prompt: "Draft an issue, but do not create it.", output: draft },
        })}
      />,
    );

    expect(container.textContent).toBe(draft);
  });

  it("keeps the created-issue identifier label for normal quick-create success", () => {
    const { container } = render(
      <InboxDetailLabel item={item({ details: { identifier: "ZIC-224" } })} />,
    );

    expect(container.textContent).toBe("Created with agent: ZIC-224");
  });
});
