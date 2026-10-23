// @vitest-environment jsdom

/**
 * The search boxes inside the Filter menu's list sections (Assignee, Creator,
 * Project, Label), driven against the REAL Base UI menu primitives — not a
 * flattened mock, because the menu popup's typeahead handler is exactly what
 * breaks these inputs: it stopEvent()s every printable key, so the search box
 * swallowed characters and the list could never be narrowed. See
 * issues-header.property-filter.test.tsx for the same rationale on the scalar
 * property input.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { Label, MemberWithUser, Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
// The avatar links into workspace-scoped routes; it is not what these tests
// exercise.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

function member(userId: string, name: string): MemberWithUser {
  return {
    id: `m-${userId}`,
    workspace_id: "ws-1",
    user_id: userId,
    role: "member",
    created_at: "",
    name,
    email: `${userId}@example.com`,
    avatar_url: null,
  };
}

const MEMBERS = [member("u-ada", "Ada Lovelace"), member("u-grace", "Grace Hopper")];

const PROJECTS = [
  { id: "p-apollo", title: "Apollo", icon: null },
  { id: "p-gemini", title: "Gemini", icon: null },
] as unknown as Project[];

const LABELS = [
  { id: "l-bug", workspace_id: "ws-1", name: "bug", color: "#ef4444" },
  { id: "l-docs", workspace_id: "ws-1", name: "docs", color: "#3b82f6" },
] as unknown as Label[];

function renderFilterMenu() {
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
    listProperties: async () => ({ properties: [] }),
    listMembers: async () => MEMBERS,
    listAgents: async () => [],
    listSquads: async () => [],
    listProjects: async () => ({ projects: PROJECTS, total: PROJECTS.length }),
    listLabels: async () => ({ labels: LABELS, total: LABELS.length }),
  } as unknown as ApiClient);

  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const store = createStore<IssueViewState>()(viewStoreSlice);

  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <IssueFilterMenu trigger={<button type="button">Filter</button>} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
}

/**
 * Opens the Filter menu, then the named list submenu, the way a user does —
 * clicks, not synthetic events — and waits for `ready`. The search box is
 * autofocused, so the returned `user` types straight into it.
 */
async function openSubmenu(section: RegExp, ready: RegExp) {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Filter" }));
  await user.click(await screen.findByRole("menuitem", { name: section }));
  await waitFor(() =>
    expect(screen.getByRole("menuitemcheckbox", { name: ready })).toBeInTheDocument(),
  );
  const input = screen.getByPlaceholderText("Filter...");
  expect(input).toHaveFocus();
  return { user, input };
}

function optionNames() {
  return screen
    .getAllByRole("menuitemcheckbox")
    .map((el) => el.textContent?.trim() ?? "");
}

afterEach(() => {
  cleanup();
  // Base UI portals the menu onto document.body; leftovers would duplicate
  // labels across tests.
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu list-section search", () => {
  it("types into the Assignee search and narrows the list", async () => {
    renderFilterMenu();
    const { user, input } = await openSubmenu(/^Assignee/, /Grace Hopper/);

    await user.keyboard("grace");

    // The regression: the menu's typeahead stopped every printable key, so the
    // value stayed "" and both members (plus "No assignee") stayed listed.
    expect(input).toHaveValue("grace");
    expect(optionNames()).toEqual(["Grace Hopper"]);
  });

  it("keeps a space typed into the Assignee search", async () => {
    renderFilterMenu();
    const { user, input } = await openSubmenu(/^Assignee/, /Grace Hopper/);

    await user.keyboard("ada l");

    expect(input).toHaveValue("ada l");
    expect(optionNames()).toEqual(["Ada Lovelace"]);
  });

  it("types into the Creator search", async () => {
    renderFilterMenu();
    const { user, input } = await openSubmenu(/^Creator/, /Ada Lovelace/);

    await user.keyboard("ada");

    expect(input).toHaveValue("ada");
    expect(optionNames()).toEqual(["Ada Lovelace"]);
  });

  it("types into the Project search", async () => {
    renderFilterMenu();
    const { user, input } = await openSubmenu(/^Project$/, /Gemini/);

    await user.keyboard("gem");

    expect(input).toHaveValue("gem");
    expect(screen.getByRole("menuitemcheckbox", { name: /Gemini/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitemcheckbox", { name: /Apollo/ })).toBeNull();
  });

  it("types into the Label search", async () => {
    renderFilterMenu();
    const { user, input } = await openSubmenu(/^Label/, /docs/);

    await user.keyboard("doc");

    expect(input).toHaveValue("doc");
    expect(optionNames()).toEqual(["docs"]);
  });

  it("keeps caret keys in the search instead of closing the submenu", async () => {
    renderFilterMenu();
    const { user, input } = await openSubmenu(/^Assignee/, /Grace Hopper/);

    await user.keyboard("grace{ArrowLeft}{Home}{End}{Meta>}{ArrowLeft}{/Meta}");

    // ArrowLeft is the submenu's close key (with or without a modifier — the
    // menu does not look at modifiers) and Home/End jump the highlight; inside
    // the search box they all belong to the caret.
    expect(input).toBeInTheDocument();
    expect(input).toHaveFocus();
    expect(input).toHaveValue("grace");
  });

  it.each([
    ["Cmd+K", "{Meta>}k{/Meta}", "k"],
    ["Ctrl+K", "{Control>}k{/Control}", "k"],
    ["Cmd+F2", "{Meta>}{F2}{/Meta}", "F2"],
    ["Ctrl+F2", "{Control>}{F2}{/Control}", "F2"],
    ["F2", "{F2}", "F2"],
    ["Ctrl+Enter", "{Control>}{Enter}{/Control}", "Enter"],
    ["Enter", "{Enter}", "Enter"],
  ])("lets a key the menu does not claim (%s) reach the document", async (_name, keys, key) => {
    renderFilterMenu();
    const { user } = await openSubmenu(/^Assignee/, /Grace Hopper/);
    // GlobalShortcuts listens for keydown on the document (Cmd/Ctrl+K opens
    // global search from editable fields, and the binding is configurable),
    // so anything the search box swallowed beyond the menu's own keys would
    // silently break a shortcut.
    const seen: KeyboardEvent[] = [];
    const listener = (event: KeyboardEvent) => {
      if (event.key === key) seen.push(event);
    };
    document.addEventListener("keydown", listener);
    try {
      await user.keyboard(keys);
    } finally {
      document.removeEventListener("keydown", listener);
    }

    expect(seen).toHaveLength(1);
  });

  it("still hands ArrowDown to the menu to step into the filtered list", async () => {
    renderFilterMenu();
    const { user } = await openSubmenu(/^Assignee/, /Grace Hopper/);

    await user.keyboard("grace{ArrowDown}");

    expect(screen.getByRole("menuitemcheckbox", { name: /Grace Hopper/ })).toHaveFocus();
  });

  it("still lets Escape close the menu from the search", async () => {
    renderFilterMenu();
    const { user } = await openSubmenu(/^Assignee/, /Grace Hopper/);

    await user.keyboard("{Escape}");

    await waitFor(() =>
      expect(screen.queryByPlaceholderText("Filter...")).not.toBeInTheDocument(),
    );
  });
});
