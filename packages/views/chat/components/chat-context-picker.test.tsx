import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Space } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";
import enIssues from "../../locales/en/issues.json";
import { ChatContextPicker } from "./chat-context-picker";

const TEST_RESOURCES = { en: { chat: enChat, issues: enIssues } };

function makeSpace(id: string, name: string, key: string): Space {
  return {
    id,
    workspace_id: "ws-1",
    name,
    key,
    icon: null,
    context: "",
    issue_counter: 0,
    is_default: false,
    visibility: "open",
    archived_at: null,
    created_by: "user-1",
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    is_member: true,
    member_role: "member",
    is_pinned: false,
    is_followed: false,
    sort_order: 0,
  };
}

const spaces = [
  makeSpace("engineering", "Engineering", "ENG"),
  makeSpace("product", "Product", "PROD"),
];

function renderPicker(value: string | null = null, onChange = vi.fn()) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatContextPicker spaces={spaces} value={value} onChange={onChange} />
    </I18nProvider>,
  );
  return onChange;
}

describe("ChatContextPicker", () => {
  it("shows All spaces and every eligible Space", async () => {
    renderPicker();
    fireEvent.click(screen.getByRole("button", { name: "Chat context: All spaces" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("All spaces")).toBeInTheDocument();
    expect(within(dialog).getByText("Can use data from 2 spaces")).toBeInTheDocument();
    expect(within(dialog).getByText("Engineering")).toBeInTheDocument();
    expect(within(dialog).getByText("Product")).toBeInTheDocument();
  });

  it("selects a concrete Space and closes the picker", async () => {
    const onChange = renderPicker(null);
    fireEvent.click(screen.getByRole("button", { name: "Chat context: All spaces" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByText("Engineering"));

    expect(onChange).toHaveBeenCalledWith("engineering");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("labels the trigger with the selected Space", () => {
    renderPicker("product");
    expect(screen.getByRole("button", { name: "Chat context: Product" })).toBeInTheDocument();
  });
});
