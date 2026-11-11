import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";

const mockConnect = vi.hoisted(() => vi.fn());
const mockRotate = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());
const mockUpdateToken = vi.hoisted(() => vi.fn());
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

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
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
    updateVCSConnectionToken: mockUpdateToken,
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { VCSConnectionRows } from "./code-vcs";

function renderRows() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <VCSConnectionRows />
    </QueryClientProvider>,
  );
}

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
  async function openTokenDialog(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: "Actions for https://git.acme.dev" }));
    await user.click(await screen.findByRole("menuitem", { name: "Update access token" }));
    return screen.findByRole("dialog", { name: "Update access token" });
  }

  it("updates only the selected connection's token and clears it after success", async () => {
    listing.current.connections = [CONNECTION];
    let finish!: (value: unknown) => void;
    mockUpdateToken.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const user = userEvent.setup();
    renderRows();
    const dialog = await openTokenDialog(user);
    const submit = within(dialog).getByRole("button", { name: "Update access token" });
    expect(submit).toBeDisabled();
    const input = within(dialog).getByLabelText("Access token");
    expect(input).toHaveAttribute("type", "password");
    await user.type(input, " replacement-token ");
    await user.click(submit);
    expect(mockUpdateToken).toHaveBeenCalledWith("ws-1", "vcs-1", { access_token: "replacement-token" });
    expect(within(dialog).getByRole("button", { name: "Updating..." })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(dialog).toBeInTheDocument();
    finish({ ...CONNECTION, account_login: "service-bot" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(toast.success).toHaveBeenCalledWith("Access token updated");
    expect(mockRotate).not.toHaveBeenCalled();
    expect(mockConnect).not.toHaveBeenCalled();
    const reopened = await openTokenDialog(user);
    expect(within(reopened).getByLabelText("Access token")).toHaveValue("");
  });

  it("keeps a rejected token available for correction and clears it when cancelled", async () => {
    listing.current.connections = [CONNECTION];
    mockUpdateToken.mockRejectedValue(new Error("the provider rejected the access token"));
    const user = userEvent.setup();
    renderRows();
    const dialog = await openTokenDialog(user);
    await user.type(within(dialog).getByLabelText("Access token"), "rejected-token");
    await user.click(within(dialog).getByRole("button", { name: "Update access token" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("the provider rejected the access token"));
    expect(within(dialog).getByLabelText("Access token")).toHaveValue("rejected-token");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const reopened = await openTokenDialog(user);
    expect(within(reopened).getByLabelText("Access token")).toHaveValue("");
  });

  it("hides connection actions from members", () => {
    listing.current = { connections: [CONNECTION], configured: true, can_manage: false };
    renderRows();
    expect(screen.getByText("Gitea · https://git.acme.dev")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("connects an instance from a dialog and then shows the one-time webhook secret", async () => {
    mockConnect.mockResolvedValue({
      ...CONNECTION,
      webhook_url: "https://api.example/webhooks/vcs/vcs-1",
      webhook_path: "/webhooks/vcs/vcs-1",
      webhook_secret: "s3cret",
    });
    const user = userEvent.setup();
    renderRows();

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
    renderRows();

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
    renderRows();

    expect(screen.getByText("MULTICA_VCS_SECRET_KEY")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });

  it("asks members to contact an admin when nothing is connected", () => {
    listing.current.can_manage = false;
    renderRows();

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/Ask a workspace admin/)).toBeInTheDocument();
  });
});
