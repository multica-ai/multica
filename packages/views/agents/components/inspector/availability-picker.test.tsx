// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Space } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";
import { AvailabilityPicker } from "./availability-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

function space(id: string, name: string, archived = false): Space {
  return {
    id,
    workspace_id: "ws-1",
    name,
    key: name.slice(0, 3).toUpperCase(),
    icon: null,
    issue_counter: 0,
    is_default: id === "space-1",
    visibility: "open",
    archived_at: archived ? "2026-07-01T00:00:00Z" : null,
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    is_member: true,
    member_role: "member",
    is_pinned: false,
    is_followed: false,
    sort_order: 1,
  };
}

const SPACES = [
  space("space-1", "Engineering"),
  space("space-2", "Design"),
  space("space-old", "Old Team", true),
];

function renderPicker(
  props: Partial<React.ComponentProps<typeof AvailabilityPicker>> = {},
) {
  const onChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AvailabilityPicker
        availabilityMode="workspace"
        availabilitySpaceIds={[]}
        permissionMode="public_to"
        invocationTargets={[
          { target_type: "workspace", target_id: "ws-1" },
        ]}
        spaces={SPACES}
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { onChange };
}

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

describe("AvailabilityPicker", () => {
  it("collects a multi-Space draft and applies it once", () => {
    const { onChange } = renderPicker();
    fireEvent.click(screen.getByRole("button", { name: /Space access/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Engineering/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Design/ }));

    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({
      availability_mode: "selected_spaces",
      availability_space_ids: ["space-1", "space-2"],
    });
  });

  it("shows archived selections by name and requires explicit cleanup", () => {
    const { onChange } = renderPicker({
      availabilityMode: "selected_spaces",
      availabilitySpaceIds: ["space-1", "space-old"],
    });
    fireEvent.click(screen.getByRole("button", { name: /Space access/ }));

    expect(screen.getByText("Old Team")).toBeInTheDocument();
    expect(screen.getByText("Archived")).toBeInTheDocument();
    const apply = screen.getByRole("button", { name: "Apply" });
    expect(apply).toBeDisabled();

    fireEvent.click(screen.getByRole("checkbox", { name: /Old Team/ }));
    expect(apply).not.toBeDisabled();
    fireEvent.click(apply);
    expect(onChange).toHaveBeenCalledWith({
      availability_mode: "selected_spaces",
      availability_space_ids: ["space-1"],
    });
  });

  it("fails safe against a server without Availability fields", () => {
    renderPicker({ availabilityMode: undefined });
    expect(screen.queryByRole("button")).toBeNull();
    expect(
      screen.getByLabelText(
        "This server does not support Agent Space access yet.",
      ),
    ).toBeInTheDocument();
  });

  it("renders a static owner-only state for non-owners", () => {
    renderPicker({ canEdit: false });
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByTestId("availability-readonly")).toBeInTheDocument();
  });
});
