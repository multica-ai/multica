/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { BriefCatchupSetting } from "./brief-catchup-setting";

const mocks = vi.hoisted(() => ({ updateWorkspace: vi.fn() }));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: mocks.updateWorkspace },
}));

const SETTINGS = {
  github_enabled: true,
  co_authored_by_enabled: false,
  custom_key: "keep me",
  brief_catchup_enabled: true,
  brief_catchup_hour: 10,
};

function workspace(settings: Record<string, unknown> = SETTINGS): Workspace {
  return {
    id: "ws-1",
    name: "Acme",
    slug: "acme",
    settings,
  } as unknown as Workspace;
}

function renderSetting(ws: Workspace, canManage = true) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  client.setQueryData(["workspaces", "list"], [ws]);
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <BriefCatchupSetting workspace={ws} canManage={canManage} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mocks.updateWorkspace.mockImplementation(async (_id: string, body: { settings: Record<string, unknown> }) => ({
    ...workspace(),
    settings: body.settings,
  }));
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("BriefCatchupSetting", () => {
  it("shows the server defaults when the workspace has not set them", () => {
    renderSetting(workspace({ github_enabled: true }));

    expect((screen.getByRole("switch", { name: "Catch up a missed brief" }) as HTMLElement).getAttribute("aria-checked")).toBe("true");
    expect((screen.getByLabelText("Catch-up hour") as HTMLSelectElement).value).toBe("10");
  });

  it("saves a new hour and keeps every other workspace setting", async () => {
    renderSetting(workspace());

    fireEvent.change(screen.getByLabelText("Catch-up hour"), { target: { value: "8" } });

    await waitFor(() => expect(mocks.updateWorkspace).toHaveBeenCalledTimes(1));
    expect(mocks.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        github_enabled: true,
        co_authored_by_enabled: false,
        custom_key: "keep me",
        brief_catchup_enabled: true,
        brief_catchup_hour: 8,
      },
    });
  });

  it("turns catch-up off and keeps the hour and the other keys", async () => {
    renderSetting(workspace());

    fireEvent.click(screen.getByRole("switch", { name: "Catch up a missed brief" }));

    await waitFor(() => expect(mocks.updateWorkspace).toHaveBeenCalledTimes(1));
    expect(mocks.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        github_enabled: true,
        co_authored_by_enabled: false,
        custom_key: "keep me",
        brief_catchup_enabled: false,
        brief_catchup_hour: 10,
      },
    });
  });

  it("disables the hour while catch-up is off", () => {
    renderSetting(workspace({ ...SETTINGS, brief_catchup_enabled: false }));

    expect((screen.getByLabelText("Catch-up hour") as HTMLSelectElement).disabled).toBe(true);
  });

  it("does not let a member change the setting", () => {
    renderSetting(workspace(), false);

    expect((screen.getByLabelText("Catch-up hour") as HTMLSelectElement).disabled).toBe(true);
    expect(screen.getByRole("switch", { name: "Catch up a missed brief" }).hasAttribute("data-disabled")).toBe(true);
  });
});
