import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";

import { CreateSquadModal } from "./create-squad";

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateSquad = vi.hoisted(() => vi.fn());
const mockAddSquadMember = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockToastWarning = vi.hoisted(() => vi.fn());

const agents = [
  {
    id: "agent-leader",
    name: "Leader Agent",
    description: "Coordinates the work",
    owner_id: "user-1",
    archived_at: null,
    runtime_id: "runtime-1",
  },
  {
    id: "agent-helper",
    name: "Helper Agent",
    description: "Helps with tasks",
    owner_id: "user-2",
    archived_at: null,
    runtime_id: "runtime-2",
  },
];

const members = [
  {
    user_id: "member-1",
    name: "Workspace Member",
    email: "member@example.test",
    avatar_url: null,
  },
];

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey.includes("agents")) return { data: agents };
    if (queryKey.includes("members")) return { data: members };
    return { data: [] };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidateQueries }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createSquad: mockCreateSquad,
    addSquadMember: mockAddSquadMember,
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/ws-test/squads/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "agents"] }),
  memberListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "members"] }),
  workspaceKeys: {
    squads: (wsId: string) => ["workspaces", wsId, "squads"],
  },
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`actor-avatar-${actorId}`} />
  ),
}));

vi.mock("../agents/components/avatar-picker", () => ({
  AvatarPicker: ({ onChange }: { onChange: (value: string) => void }) => (
    <button type="button" onClick={() => onChange("https://cdn.example.test/squad.png")}>
      Pick avatar
    </button>
  ),
}));

vi.mock("../agents/components/char-counter", () => ({
  CharCounter: ({ length, max }: { length: number; max: number }) => (
    <span>{`${length}/${max}`}</span>
  ),
}));

vi.mock("../issues/components/pickers/property-picker", () => ({
  PickerEmpty: () => <div>No results</div>,
  PickerItem: ({
    children,
    onClick,
    selected,
  }: {
    children: ReactNode;
    onClick: () => void;
    selected?: boolean;
  }) => (
    <button type="button" aria-pressed={selected} onClick={onClick}>
      {children}
    </button>
  ),
  PickerSection: ({ children, label }: { children: ReactNode; label: string }) => (
    <section aria-label={label}>{children}</section>
  ),
}));

vi.mock("../editor/extensions/pinyin-match", () => ({
  matchesPinyin: () => false,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <>{children}</>,
  PopoverTrigger: ({
    children,
    render,
  }: {
    children?: ReactNode;
    render?: ReactNode;
  }) => <>{render ?? children}</>,
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
  }) => (
    <button type="button" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
    warning: mockToastWarning,
  },
}));

function renderModal() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, modals: enModals } }}>
      <CreateSquadModal onClose={vi.fn()} />
    </I18nProvider>,
  );
}

describe("CreateSquadModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateSquad.mockResolvedValue({ id: "squad-1" });
    mockAddSquadMember.mockResolvedValue({ id: "member-row-1" });
  });

  it("creates a squad with avatar and adds selected members", async () => {
    const user = userEvent.setup();
    renderModal();

    await user.click(screen.getByRole("button", { name: "Pick avatar" }));
    await user.type(screen.getByPlaceholderText("e.g. Frontend Team"), "Platform Squad");
    await user.type(
      screen.getByPlaceholderText("Describe what this squad is responsible for..."),
      "Owns platform work",
    );

    await user.click(screen.getAllByRole("button", { name: /Leader Agent/ })[0]!);
    await user.click(screen.getByRole("button", { name: /Workspace Member/ }));
    await user.click(screen.getByRole("button", { name: "Create Squad" }));

    await waitFor(() =>
      expect(mockCreateSquad).toHaveBeenCalledWith({
        name: "Platform Squad",
        description: "Owns platform work",
        leader_id: "agent-leader",
        avatar_url: "https://cdn.example.test/squad.png",
      }),
    );
    expect(mockAddSquadMember).toHaveBeenCalledWith("squad-1", {
      member_type: "member",
      member_id: "member-1",
    });
    expect(mockPush).toHaveBeenCalledWith("/ws-test/squads/squad-1");
  });
});
