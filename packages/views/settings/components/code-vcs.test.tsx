import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import type { VCSConnection } from "@multica/core/types";
import { toast } from "sonner";

const mockConnect = vi.hoisted(() => vi.fn());
const mockRotate = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());
const mockCopyText = vi.hoisted(() => vi.fn());
const listing = vi.hoisted(() => ({
  current: {
    connections: [] as VCSConnection[],
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
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: mockCopyText }));

vi.mock("./gongfeng-repository-picker", () => ({ GongfengRepositoryPicker: ({ connection }: { connection: { instance_url: string } }) => <div role="dialog" aria-label="Gongfeng repositories">{connection.instance_url}</div> }));

import { VCSConnectionRows } from "./code-vcs";

const CONNECTION = {
  id: "vcs-1",
  provider: "gitea" as const,
  instance_url: "https://git.acme.dev",
  account_login: "bot",
  workspace_id: "ws-1",
  webhook_url: "https://hooks.example/api/webhooks/vcs/vcs-1",
  webhook_path: "/api/webhooks/vcs/vcs-1",
  created_at: "",
};

beforeEach(() => {
  vi.clearAllMocks();
  mockDelete.mockResolvedValue({ webhook_cleanup_error: "" });
  mockCopyText.mockResolvedValue(true);
  listing.current = { connections: [], configured: true, can_manage: true };
});

describe("VCSConnectionRows", () => {
  it("lets an admin view and copy an existing Gongfeng webhook without rotating its secret", async () => {
    listing.current.connections = [{ ...CONNECTION, provider: "gongfeng", instance_url: "https://git.code.tencent.com" }];
    const user = userEvent.setup();
    renderWithI18n(<VCSConnectionRows />);
    await user.click(screen.getByRole("button", { name: "Actions for https://git.code.tencent.com" }));
    await user.click(await screen.findByRole("menuitem", { name: "View webhook" }));
    const dialog = await screen.findByRole("dialog", { name: "Webhook" });
    expect(within(dialog).getByLabelText("Webhook URL")).toHaveValue(CONNECTION.webhook_url);
    expect(within(dialog).queryByLabelText("Webhook secret")).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Copy: Webhook URL" }));
    expect(mockCopyText).toHaveBeenCalledWith(CONNECTION.webhook_url);
    expect(toast.success).toHaveBeenCalledWith("Copied to clipboard");
    expect(mockRotate).not.toHaveBeenCalled();
    expect(mockConnect).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole("button", { name: "Gongfeng repositories" }));
    expect(await screen.findByRole("dialog", { name: "Gongfeng repositories" })).toBeVisible();
  });

  it("shows a copy error when the shared clipboard helper cannot copy the webhook", async () => {
    listing.current.connections = [CONNECTION];
    mockCopyText.mockResolvedValue(false);
    const user = userEvent.setup();
    renderWithI18n(<VCSConnectionRows />);
    await user.click(screen.getByRole("button", { name: "Actions for https://git.acme.dev" }));
    await user.click(await screen.findByRole("menuitem", { name: "View webhook" }));
    await user.click(within(await screen.findByRole("dialog", { name: "Webhook" })).getByRole("button", { name: "Copy: Webhook URL" }));
    expect(mockCopyText).toHaveBeenCalledWith(CONNECTION.webhook_url);
    expect(toast.error).toHaveBeenCalledWith("Could not copy");
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("shows the receiver path when a connection has no public webhook URL", async () => {
    listing.current.connections = [{ ...CONNECTION, webhook_url: "" }];
    const user = userEvent.setup();
    renderWithI18n(<VCSConnectionRows />);
    await user.click(screen.getByRole("button", { name: "Actions for https://git.acme.dev" }));
    await user.click(await screen.findByRole("menuitem", { name: "View webhook" }));
    const dialog = await screen.findByRole("dialog", { name: "Webhook" });
    expect(within(dialog).getByLabelText("Webhook URL")).toHaveValue(CONNECTION.webhook_path);
    await user.click(within(dialog).getByText("Close", { selector: "button", exact: true }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mockRotate).not.toHaveBeenCalled();
  });

  it("connects official Gongfeng and opens repository selection", async () => {
    mockConnect.mockResolvedValue({
      ...CONNECTION,
      provider: "gongfeng",
      instance_url: "https://git.code.tencent.com",
      webhook_url: "https://api.example/api/webhooks/vcs/vcs-1",
      webhook_secret: "gongfeng-secret",
    });
    const user = userEvent.setup();
    renderWithI18n(<VCSConnectionRows />);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    const form = await screen.findByRole("dialog");
    await user.click(within(form).getByRole("combobox"));
    await user.click(await screen.findByRole("option", { name: "Tencent Gongfeng" }));
    expect(within(form).getByLabelText("Instance URL")).toHaveValue("https://git.code.tencent.com");
    expect(within(form).getByText(/Create a Private Token/)).toBeInTheDocument();
    await user.type(within(form).getByLabelText("Access token"), "tok");
    await user.click(within(form).getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(mockConnect).toHaveBeenCalledWith("ws-1", {
      provider: "gongfeng", instance_url: "https://git.code.tencent.com", access_token: "tok",
    }));
    const picker = await screen.findByRole("dialog", { name: "Gongfeng repositories" });
    expect(within(picker).getByText("https://git.code.tencent.com")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("gongfeng-secret")).not.toBeInTheDocument();
  });

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
