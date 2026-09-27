import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";

const mocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  connectVCS: vi.fn(),
  invalidateQueries: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: mocks.success, error: mocks.error },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Workspace" }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  return {
    useAuthStore: Object.assign(
      (selector: (value: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("@multica/core/api", () => ({ api: { connectVCS: mocks.connectVCS } }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  invitationListOptions: () => ({ queryKey: ["invitations"] }),
  shareLinkListOptions: () => ({ queryKey: ["share-links"] }),
  workspaceKeys: { all: ["workspaces"] },
}));
vi.mock("@multica/core/billing", () => ({
  workspaceSubscriptionSummaryOptions: () => ({ queryKey: ["billing"] }),
  usePreviewWorkspaceSeatPurchase: () => ({}),
  usePurchaseWorkspaceSeats: () => ({}),
}));
vi.mock("@multica/core/vcs", () => ({
  vcsConnectionsOptions: () => ({ queryKey: ["vcs"] }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    switch (queryKey[0]) {
      case "members":
        return {
          data: [
            { id: "member-1", user_id: "user-1", role: "owner", name: "Owner" },
          ],
        };
      case "share-links":
        return {
          data: [
            { id: "link-1", code: "invite-code", role: "member", use_count: 0 },
          ],
        };

      case "vcs":
        return {
          data: { connections: [], configured: true, can_manage: true },
        };
      default:
        return { data: [] };
    }
  },
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

import { MembersTab } from "./members-tab";
import { VCSTab } from "./vcs-tab";

const secureContextDescriptor = Object.getOwnPropertyDescriptor(
  window,
  "isSecureContext",
);
const clipboardDescriptor = Object.getOwnPropertyDescriptor(
  navigator,
  "clipboard",
);
const execCommandDescriptor = Object.getOwnPropertyDescriptor(
  document,
  "execCommand",
);

function setClipboard(writeText?: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: writeText ? { writeText } : undefined,
  });
}

function setLegacyCopy(result: boolean) {
  const copy = vi.fn(() => result);
  Object.defineProperty(document, "execCommand", {
    configurable: true,
    value: copy,
  });
  return copy;
}

function renderMembers() {
  renderWithI18n(
    <NavigationProvider
      value={{
        push: vi.fn(),
        replace: vi.fn(),
        back: vi.fn(),
        pathname: "/settings",
        searchParams: new URLSearchParams(),
        hash: "",
        getShareableUrl: (path) => `https://public.example${path}`,
      }}
    >
      <MembersTab />
    </NavigationProvider>,
  );
}

async function connectVCS() {
  mocks.connectVCS.mockResolvedValue({
    id: "connection-1",
    webhook_url: "https://public.example/webhook",
    webhook_path: "/webhook",
    webhook_secret: "test-only-secret",
  });
  renderWithI18n(<VCSTab />);
  fireEvent.change(screen.getByLabelText("Instance URL"), {
    target: { value: "https://forgejo.example" },
  });
  fireEvent.change(screen.getByLabelText("Access token"), {
    target: { value: "test-only-token" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Connect" }));
  await screen.findByDisplayValue("https://public.example/webhook");
  mocks.success.mockClear();
}

beforeEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(window, "isSecureContext", {
    configurable: true,
    value: true,
  });
  setClipboard();
});

afterEach(() => {
  if (secureContextDescriptor)
    Object.defineProperty(window, "isSecureContext", secureContextDescriptor);
  else Reflect.deleteProperty(window, "isSecureContext");
  if (clipboardDescriptor)
    Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  else Reflect.deleteProperty(navigator, "clipboard");
  if (execCommandDescriptor)
    Object.defineProperty(document, "execCommand", execCommandDescriptor);
  else Reflect.deleteProperty(document, "execCommand");
});

// The clipboard API matrix lives in editor/utils/clipboard.test.ts; these cover settings wiring.
describe("settings clipboard feedback", () => {
  it("does not report an invite link copied when legacy copying returns false", async () => {
    const legacyCopy = setLegacyCopy(false);
    renderMembers();
    fireEvent.click(screen.getByTitle("Copy link"));
    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith("Failed to copy link"),
    );
    expect(legacyCopy).toHaveBeenCalledWith("copy");
    expect(mocks.success).not.toHaveBeenCalled();
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("copies the public invite URL through the navigation adapter", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setClipboard(writeText);
    renderMembers();
    fireEvent.click(screen.getByTitle("Copy link"));
    await waitFor(() =>
      expect(mocks.success).toHaveBeenCalledWith("Link copied"),
    );
    expect(writeText).toHaveBeenCalledWith(
      "https://public.example/join?code=invite-code",
    );
    expect(mocks.error).not.toHaveBeenCalled();
  });

  it("copies a VCS webhook when the Clipboard API is unavailable", async () => {
    setClipboard();
    const legacyCopy = setLegacyCopy(true);
    await connectVCS();
    fireEvent.click(screen.getAllByTitle("Copy")[0]!);
    await waitFor(() =>
      expect(mocks.success).toHaveBeenCalledWith("Copied to clipboard"),
    );

    expect(legacyCopy).toHaveBeenCalledWith("copy");
    expect(mocks.error).not.toHaveBeenCalled();
  });

  it("reports a VCS copy failure when clipboard permission and the legacy fallback both fail", async () => {
    setClipboard(vi.fn().mockRejectedValue(new Error("Permission denied")));
    const legacyCopy = setLegacyCopy(false);
    await connectVCS();
    fireEvent.click(screen.getAllByTitle("Copy")[1]!);
    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith("Could not copy"),
    );
    expect(legacyCopy).toHaveBeenCalledWith("copy");
    expect(mocks.success).not.toHaveBeenCalled();
  });
});
