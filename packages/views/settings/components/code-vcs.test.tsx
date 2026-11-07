import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const mockConnect = vi.hoisted(() => vi.fn());
const mockRotate = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());
const listing = vi.hoisted(() => ({
  current: {
    connections: [] as {
      id: string;
      provider: "forgejo" | "gitea" | "gitlab";
      instance_url: string;
      account_login: string;
    }[],
    configured: true,
    can_manage: true,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: listing.current }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: {
    connectVCS: mockConnect,
    rotateVCSWebhook: mockRotate,
    deleteVCSConnection: mockDelete,
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { VCSConnectionRows } from "./code-vcs";

const CONNECTION = {
  id: "vcs-1",
  provider: "gitea" as const,
  instance_url: "https://git.acme.dev",
  account_login: "bot",
};

beforeEach(() => {
  vi.clearAllMocks();
  listing.current = { connections: [], configured: true, can_manage: true };
});

describe("VCSConnectionRows", () => {
  it("connects an instance from a dialog and then shows the one-time webhook secret", async () => {
    mockConnect.mockResolvedValue({
      ...CONNECTION,
      webhook_url: "https://api.example/webhooks/vcs/vcs-1",
      webhook_path: "/webhooks/vcs/vcs-1",
      webhook_secret: "s3cret",
    });
    const user = userEvent.setup();
    renderWithI18n(<VCSConnectionRows />);

    await user.click(screen.getByRole("button", { name: "Connect" }));
    const form = await screen.findByRole("dialog");
    await user.type(within(form).getByLabelText("Instance URL"), " https://git.acme.dev ");
    await user.type(within(form).getByLabelText("Access token"), "tok");
    await user.click(within(form).getByRole("button", { name: "Connect" }));

    await waitFor(() =>
      expect(mockConnect).toHaveBeenCalledWith("ws-1", {
        provider: "forgejo",
        instance_url: "https://git.acme.dev",
        access_token: "tok",
      }),
    );
    const webhook = await screen.findByRole("dialog", { name: /Finish setup/ });
    expect(within(webhook).getByDisplayValue("s3cret")).toBeInTheDocument();
    await user.click(within(webhook).getByRole("button", { name: "I've saved it" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("lists connected instances with their actions", async () => {
    listing.current.connections = [CONNECTION];
    const user = userEvent.setup();
    renderWithI18n(<VCSConnectionRows />);

    expect(screen.getByText("Gitea · https://git.acme.dev")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Actions for https://git.acme.dev" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Regenerate webhook" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Disconnect" })).toBeInTheDocument();
    // Another instance can still be added.
    expect(screen.getByRole("button", { name: "Connect another" })).toBeInTheDocument();
  });

  it("explains the missing server key instead of offering a dead connect", () => {
    listing.current.configured = false;
    renderWithI18n(<VCSConnectionRows />);

    expect(screen.getByText("MULTICA_VCS_SECRET_KEY")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });

  it("asks members to contact an admin when nothing is connected", () => {
    listing.current.can_manage = false;
    renderWithI18n(<VCSConnectionRows />);

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/Ask a workspace admin/)).toBeInTheDocument();
  });
});
