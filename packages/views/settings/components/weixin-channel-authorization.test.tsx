// @vitest-environment jsdom
import { StrictMode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { ApiError } from "@multica/core/api/client";

const mocks = vi.hoisted(() => ({
  begin: vi.fn(),
  poll: vi.fn(),
  cancel: vi.fn(),
  toast: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({
  api: {
    beginWeixinLogin: mocks.begin,
    pollWeixinLogin: mocks.poll,
    cancelWeixinLogin: mocks.cancel,
  },
  clientErrorMessage: () => undefined,
}));
vi.mock("sonner", () => ({ toast: { success: mocks.toast } }));

import { WeixinChannelAuthorization } from "./weixin-channel-authorization";

const login = {
  id: "login-1",
  status: "wait",
  qr_code_url: "https://weixin.qq.com/test-qr",
  expires_at: new Date(Date.now() + 300_000).toISOString(),
};

function renderAuthorization(onClose = vi.fn(), onOpenGuide = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const result = render(
    <StrictMode>
      <QueryClientProvider client={client}>
        <I18nProvider
          locale="en"
          resources={{ en: { common: enCommon, settings: enSettings } }}
        >
          <WeixinChannelAuthorization
            wsId="workspace-1"
            agentId="agent-1"
            onOpenGuide={onOpenGuide}
            onClose={onClose}
          />
        </I18nProvider>
      </QueryClientProvider>
    </StrictMode>,
  );
  return { ...result, onClose, onOpenGuide, invalidate };
}

describe("WeChat QR connection", () => {
  beforeEach(() => {
    cleanup();
    vi.resetAllMocks();
    mocks.begin.mockResolvedValue(login);
    mocks.poll.mockResolvedValue(login);
    mocks.cancel.mockResolvedValue(undefined);
  });

  it("shows the QR on open with the guide below it, requesting one login", async () => {
    const user = userEvent.setup();
    const { onOpenGuide } = renderAuthorization();
    const qr = await screen.findByTitle("WeChat connection QR code");
    const guide = screen.getByRole("button", { name: "Setup guide" });
    expect(
      qr.compareDocumentPosition(guide) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    await user.click(guide);
    expect(onOpenGuide).toHaveBeenCalledTimes(1);
    expect(mocks.begin).toHaveBeenCalledTimes(1);
    expect(mocks.begin).toHaveBeenCalledWith("workspace-1", "agent-1");
    expect(
      screen.queryByRole("button", { name: "Connect WeChat" }),
    ).not.toBeInTheDocument();
  });

  it.each(["need_verifycode", "confirmed_but_need_verifycode"])(
    "submits the phone code for %s, then announces the connection and closes",
    async (status) => {
      const user = userEvent.setup();
      mocks.poll
        .mockResolvedValueOnce({ ...login, status })
        .mockResolvedValue({
          id: login.id,
          status: "confirmed",
          installation_id: "bot-1",
          expires_at: login.expires_at,
        });
      const { onClose, invalidate } = renderAuthorization();
      await user.type(
        await screen.findByLabelText("Verification code"),
        "001234",
      );
      await user.click(screen.getByRole("button", { name: "Verify" }));
      await waitFor(() =>
        expect(mocks.poll).toHaveBeenCalledWith(
          "workspace-1",
          "login-1",
          "001234",
        ),
      );
      await waitFor(() => expect(onClose).toHaveBeenCalled());
      expect(mocks.toast).toHaveBeenCalledTimes(1);
      expect(mocks.toast).toHaveBeenCalledWith(
        "Connected. Send a new message to the bot in WeChat to start.",
      );
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ["weixin", "workspace-1", "installations"],
      });
      expect(mocks.cancel).not.toHaveBeenCalled();
    },
  );

  it("cancels the pending login when the dialog closes", async () => {
    const { unmount } = renderAuthorization();
    await screen.findByTitle("WeChat connection QR code");
    unmount();
    expect(mocks.cancel).toHaveBeenCalledWith("workspace-1", "login-1");
  });

  it("cancels a login that arrives after the dialog closed", async () => {
    let resolve!: (value: typeof login) => void;
    mocks.begin.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const { unmount } = renderAuthorization();
    expect(await screen.findByRole("status")).toHaveTextContent("Connecting…");
    unmount();
    resolve(login);
    await waitFor(() =>
      expect(mocks.cancel).toHaveBeenCalledWith("workspace-1", "login-1"),
    );
  });

  it("stops on expired QR and lets the user request a new one", async () => {
    const user = userEvent.setup();
    mocks.poll.mockResolvedValue({
      ...login,
      status: "expired",
      qr_code_url: undefined,
    });
    renderAuthorization();
    expect(
      await screen.findByText("This QR code has expired. Generate a new one."),
    ).toBeVisible();
    expect(
      screen.queryByTitle("WeChat connection QR code"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "New QR code" }));
    await waitFor(() => expect(mocks.begin).toHaveBeenCalledTimes(2));
  });

  it("names the agent owning a conflicting WeChat connection", async () => {
    mocks.poll.mockRejectedValue(
      new ApiError("already connected", 409, "Conflict", {
        code: "weixin_already_connected",
        agent_id: "other-agent",
        agent_name: "Research Assistant",
      }),
    );
    renderAuthorization();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This WeChat account is connected to Research Assistant. Disconnect it there first.",
    );
    expect(
      screen.queryByTitle("WeChat connection QR code"),
    ).not.toBeInTheDocument();
  });

  it("keeps cross-workspace conflict details private", async () => {
    mocks.poll.mockRejectedValue(
      new ApiError("already connected", 409, "Conflict", {
        code: "weixin_already_connected",
      }),
    );
    renderAuthorization();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This WeChat account is connected elsewhere. Disconnect the existing connection first.",
    );
  });
});
