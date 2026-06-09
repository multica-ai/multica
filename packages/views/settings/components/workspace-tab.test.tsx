import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
// FIR-2580: AI logo generation. flagRef lets a test enable the logo feature
// without swapping the whole mock; generateWorkspaceLogos is the new API method.
const flagRef = vi.hoisted(() => ({ current: false }));
const mockGenerateWorkspaceLogos = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    description: "",
    context: "",
    settings: {} as Record<string, unknown>,
    issue_prefix: "TES",
    repos: [] as { url: string }[],
  },
}));
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as "owner" | "admin" | "member" }],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: membersRef.current, isFetched: true }),
  useQueryClient: () => ({
    setQueryData: vi.fn(),
    getQueryData: vi.fn(() => []),
    invalidateQueries: mockInvalidateQueries,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  workspaceListOptions: () => ({ queryKey: ["workspaces"], queryFn: vi.fn() }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueKeys: { all: (wsId: string) => ["issues", wsId] },
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useLeaveWorkspace: () => ({ mutateAsync: vi.fn() }),
  useDeleteWorkspace: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: mockUpdateWorkspace,
    generateWorkspaceLogos: mockGenerateWorkspaceLogos,
    getBaseUrl: () => "http://127.0.0.1:8080",
  },
}));

// FIR-2580: the logo uploader pulls in these two helpers when the flag is on.
// Stub them so the AI-generation test stays focused on the uploader's behaviour.
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: vi.fn(), uploading: false }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null | undefined) => url || null,
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("./delete-workspace-dialog", () => ({
  DeleteWorkspaceDialog: () => null,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// FIR-2580: stub the feature flag (default off) so the test avoids the
// cerebro-feature-flags store import chain (which calls platform's
// registerForWorkspaceRehydration at module load). The logo uploader stays
// hidden, matching the flag's default-off behaviour.
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => flagRef.current,
}));

import { WorkspaceTab } from "./workspace-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("WorkspaceTab — issue prefix editing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    flagRef.current = false;
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      description: "",
      context: "",
      settings: {},
      issue_prefix: "TES",
      repos: [],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    mockUpdateWorkspace.mockImplementation(
      async (
        _id: string,
        payload: { issue_prefix?: string; name?: string; settings?: Record<string, unknown> },
      ) => ({
        ...workspaceRef.current,
        ...payload,
        issue_prefix: payload.issue_prefix ?? workspaceRef.current.issue_prefix,
      }),
    );
  });

  it("renders the current prefix in the input", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;
    expect(input.value).toBe("TES");
  });

  it("uppercases and strips non-alphanumeric input as the user types", async () => {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "ab-12!cd");

    expect(input.value).toBe("AB12CD");
  });

  it("saves directly without confirm when the prefix is unchanged", async () => {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1);
    });
    // No issue_prefix in the payload when unchanged — avoids no-op churn
    // and keeps the request shape identical to pre-feature behavior.
    expect(mockUpdateWorkspace).toHaveBeenCalledWith(
      "workspace-1",
      expect.not.objectContaining({ issue_prefix: expect.anything() }),
    );
    expect(screen.queryByText(/Change issue prefix/i)).toBeNull();
    // Non-prefix saves must NOT invalidate the issue cache — would
    // trigger an unnecessary workspace-wide refetch on every name edit.
    expect(mockInvalidateQueries).not.toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["issues", "workspace-1"] }),
    );
  });

  it("shows a confirm dialog before saving when the prefix changes, and only saves on confirm", async () => {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;
    await user.clear(input);
    await user.type(input, "NEW");

    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    // Save is gated behind the dialog — no API call yet.
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();

    // Dialog body mentions both the old and new prefix in the warning.
    await screen.findByText(/Change issue prefix/i);
    expect(screen.getByText(/TES-N/)).toBeTruthy();
    expect(screen.getByText(/NEW-N/)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1);
    });
    expect(mockUpdateWorkspace).toHaveBeenCalledWith(
      "workspace-1",
      expect.objectContaining({ issue_prefix: "NEW" }),
    );
    // Issue identifiers (`MUL-123`) are recomputed from the workspace
    // prefix at read time, so cached issues display the stale OLD-N key
    // until invalidated. Without this the confirm dialog's promise that
    // "all issues will be renumbered to NEW-N" is a lie.
    expect(mockInvalidateQueries).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["issues", "workspace-1"] }),
    );
  });

  it("cancelling the confirm dialog does not save", async () => {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;
    await user.clear(input);
    await user.type(input, "NEW");

    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    await screen.findByText(/Change issue prefix/i);
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    // The user's edited value is preserved so they can resume.
    expect(input.value).toBe("NEW");
  });

  it("disables Save when the prefix is empty", async () => {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;
    await user.clear(input);

    expect(screen.getByRole("button", { name: /^Save$/ })).toBeDisabled();
  });

  it("disables the prefix input for non-admins", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    expect(screen.getByPlaceholderText("TES")).toBeDisabled();
  });

  it("saves the started issues inbox preference", async () => {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("switch", { name: /Put newly started issues in the creator's Inbox/i }));
    await user.click(screen.getByRole("button", { name: /^Save workflow$/ }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith(
        "workspace-1",
        expect.objectContaining({
          settings: expect.objectContaining({ started_issues_in_inbox: true }),
        }),
      );
    });
  });
});

// FIR-2580 — AI logo generation flow (feature flag on).
describe("WorkspaceTab — AI logo generation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    flagRef.current = true;
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      description: "",
      context: "",
      settings: {},
      issue_prefix: "TES",
      repos: [],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    mockUpdateWorkspace.mockImplementation(async (_id: string, payload: Record<string, unknown>) => ({
      ...workspaceRef.current,
      ...payload,
    }));
  });

  it("opens the modal with a name-seeded prompt, generates 5 options, and saves the chosen one", async () => {
    const user = userEvent.setup();
    mockGenerateWorkspaceLogos.mockResolvedValue({
      urls: ["u1.png", "u2.png", "u3.png", "u4.png", "u5.png"],
    });

    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    // The AI trigger sits next to the logo upload square for owners/admins.
    await user.click(screen.getByRole("button", { name: "Generate with AI" }));

    // Modal pre-fills a suggestion prompt seeded from the workspace name.
    const promptBox = (await screen.findByLabelText("Describe your logo")) as HTMLTextAreaElement;
    expect(promptBox.value).toContain("Test Workspace");

    // One click fans out to the generator with the prompt and a count of 5.
    await user.click(screen.getByRole("button", { name: "Generate 5 options" }));
    await waitFor(() => {
      expect(mockGenerateWorkspaceLogos).toHaveBeenCalledWith(
        "workspace-1",
        expect.stringContaining("Test Workspace"),
        5,
      );
    });

    // Five selectable options render; picking one enables "Use selected logo".
    const options = await screen.findAllByRole("button", { name: /^Logo option \d$/ });
    expect(options).toHaveLength(5);
    await user.click(options[1]!);

    await user.click(screen.getByRole("button", { name: "Use selected logo" }));
    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith(
        "workspace-1",
        expect.objectContaining({ avatar_url: "u2.png" }),
      );
    });
  });

  it("does not show the logo uploader (upload area or AI trigger) for non-managers", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    expect(screen.queryByText("Workspace logo")).toBeNull();
    expect(screen.queryByRole("button", { name: "Generate with AI" })).toBeNull();
  });
});
