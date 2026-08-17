// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../../test/i18n";
import { PillButton } from "../../../common/pill-button";
import { AssigneePicker } from "./assignee-picker";
import { PriorityPicker } from "./priority-picker";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: null }) => unknown) =>
    selector({ user: null }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Unknown" }),
}));

afterEach(cleanup);

describe("deferred picker triggers", () => {
  it("renders generated content when triggerRender only supplies an empty shell", () => {
    renderWithI18n(
      <>
        <PriorityPicker
          priority="none"
          onUpdate={() => {}}
          triggerRender={<PillButton />}
        />
        <AssigneePicker
          assigneeType={null}
          assigneeId={null}
          onUpdate={() => {}}
          triggerRender={<PillButton />}
        />
      </>,
    );

    expect(
      screen.getByRole("button", { name: "No priority" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Unassigned" }),
    ).toBeInTheDocument();
  });
});
