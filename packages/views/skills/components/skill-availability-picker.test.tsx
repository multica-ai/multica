// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Space } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import enSkills from "../../locales/en/skills.json";
import { SkillAvailabilityPicker } from "./skill-availability-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, issues: enIssues, skills: enSkills },
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
  props: Partial<React.ComponentProps<typeof SkillAvailabilityPicker>> = {},
) {
  const onChange = vi.fn().mockResolvedValue(undefined);
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <SkillAvailabilityPicker
        mode="private"
        selectedSpaceIds={[]}
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

describe("SkillAvailabilityPicker", () => {
  it("applies a Selected Spaces sharing scope atomically", async () => {
    const { onChange } = renderPicker();
    fireEvent.click(screen.getByRole("button", { name: "Change Skill sharing scope" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Engineering/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Design/ }));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith({
        availability_mode: "selected_spaces",
        availability_space_ids: ["space-1", "space-2"],
      }),
    );
  });

  it("requires archived selections to be removed before applying", () => {
    renderPicker({
      mode: "selected_spaces",
      selectedSpaceIds: ["space-1", "space-old"],
    });
    fireEvent.click(screen.getByRole("button", { name: "Change Skill sharing scope" }));

    expect(screen.getByText("Old Team")).toBeInTheDocument();
    const apply = screen.getByRole("button", { name: "Apply" });
    expect(apply).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: /Old Team/ }));
    expect(apply).not.toBeDisabled();
  });

  it("renders a static scope for users who cannot edit", () => {
    renderPicker({ mode: "workspace", canEdit: false });
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("Workspace")).toBeInTheDocument();
  });
});
