// @vitest-environment jsdom

/**
 * The Labels search inside the Skills page's Filter menu, driven against the
 * REAL Base UI menu primitives: the menu popup's typeahead stopEvent()s every
 * printable key, so without a keyboard guard on the search box it swallowed
 * every character. Same defect and guard as the issues Filter menu — see
 * issues/components/issues-header.advanced-filter.test.tsx.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { EMPTY_SKILL_FILTERS } from "@multica/core/skills/stores";
import type { Label } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { SkillFilterMenu } from "./skill-list-controls";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const LABELS = [
  { id: "l-bug", workspace_id: "ws-1", name: "bug", color: "#ef4444" },
  { id: "l-docs", workspace_id: "ws-1", name: "docs", color: "#3b82f6" },
] as unknown as Label[];

function renderFilterMenu() {
  setApiInstance({
    listLabels: async () => ({ labels: LABELS, total: LABELS.length }),
  } as unknown as ApiClient);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <SkillFilterMenu
        filters={EMPTY_SKILL_FILTERS}
        onToggleFilter={() => {}}
        rows={[]}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  // Base UI portals the menu onto document.body.
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("SkillFilterMenu label filter search", () => {
  it("types into the Labels search and narrows the list", async () => {
    renderFilterMenu();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^Filter/ }));
    await user.click(await screen.findByRole("menuitem", { name: /^Labels/ }));
    await waitFor(() =>
      expect(screen.getByRole("menuitemcheckbox", { name: /docs/ })).toBeInTheDocument(),
    );
    const input = screen.getByPlaceholderText("Filter...");
    expect(input).toHaveFocus();

    await user.keyboard("doc");

    expect(input).toHaveValue("doc");
    expect(screen.getByRole("menuitemcheckbox", { name: /docs/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitemcheckbox", { name: /bug/ })).toBeNull();

    // Left/right/Home/End edit the query instead of closing the submenu.
    await user.keyboard("{ArrowLeft}{Home}{End}{Backspace}");
    expect(input).toHaveValue("do");
    expect(input).toHaveFocus();

    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByPlaceholderText("Filter...")).not.toBeInTheDocument());
  });
});
