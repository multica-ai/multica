// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, MemberWithUser, Squad, SquadMember } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSquads from "../../locales/en/squads.json";

const TEST_RESOURCES = {
  en: { common: enCommon, squads: enSquads },
};

const OWNER_USER = "user-owner";
const CREATOR_USER = "user-creator";
const BYSTANDER_USER = "user-bystander";

const fakeSquad: Squad = {
  id: "squad-1",
  workspace_id: "ws-1",
  name: "FakeSquad",
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "agent-1",
  creator_id: CREATOR_USER,
  created_at: "2026-05-17T00:00:00Z",
  updated_at: "2026-05-17T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const fakeMembers: SquadMember[] = [
  {
    id: "sm-1",
    squad_id: "squad-1",
    member_type: "agent",
    member_id: "agent-1",
    role: "leader",
    created_at: "2026-05-17T00:00:00Z",
  },
];

const fakeWsMembers: MemberWithUser[] = [
  {
    id: "mwu-owner",
    user_id: OWNER_USER,
    workspace_id: "ws-1",
    role: "owner",
    name: "Owner",
    avatar_url: null,
    email: "o@x",
    created_at: "2026-05-17T00:00:00Z",
  },
  {
    id: "mwu-creator",
    user_id: CREATOR_USER,
    workspace_id: "ws-1",
    role: "member",
    name: "Creator",
    avatar_url: null,
    email: "c@x",
    created_at: "2026-05-17T00:00:00Z",
  },
  {
    id: "mwu-bystander",
    user_id: BYSTANDER_USER,
    workspace_id: "ws-1",
    role: "member",
    name: "Bystander",
    avatar_url: null,
    email: "b@x",
    created_at: "2026-05-17T00:00:00Z",
  },
];

const fakeAgents: Agent[] = [
  {
    id: "agent-1",
    workspace_id: "ws-1",
    name: "LeaderAgent",
    description: "",
    visibility: "private",
    avatar_url: null,
    runtime_id: "rt-1",
    runtime_mode: "cloud",
    runtime_config: {},
    owner_id: CREATOR_USER,
    instructions: "",
    custom_env: {},
    custom_env_redacted: false,
    custom_args: [],
    skills: [],
    status: "idle",
    model: "",
    created_at: "2026-05-17T00:00:00Z",
    updated_at: "2026-05-17T00:00:00Z",
    archived_at: null,
    archived_by: null,
    max_concurrent_tasks: 1,
  },
];

const mocks = vi.hoisted(() => ({
  authUserId: "user-owner" as string,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    const keyStr = Array.isArray(key) ? key.join("/") : "";
    // squad members: ends with squads/ws-1/squads/squad-1/members
    if (keyStr.includes("squads") && keyStr.endsWith("members")) {
      return { data: fakeMembers, refetch: () => {} };
    }
    // squad detail: contains squad-1
    if (keyStr.includes("squads") && keyStr.includes("squad-1")) {
      return { data: fakeSquad, refetch: () => {} };
    }
    // agents list
    if (keyStr.includes("agents")) {
      return { data: fakeAgents };
    }
    // members list
    if (keyStr.includes("members")) {
      return { data: fakeWsMembers };
    }
    // runtimes
    return { data: [], refetch: () => {} };
  },
  useMutation: () => ({
    mutate: () => {},
    mutateAsync: async () => {},
    isPending: false,
  }),
  useQueryClient: () => ({
    setQueryData: () => {},
    invalidateQueries: () => {},
  }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getSquad: vi.fn(),
    listSquadMembers: vi.fn(),
    updateSquad: vi.fn(),
    deleteSquad: vi.fn(),
    addSquadMember: vi.fn(),
    removeSquadMember: vi.fn(),
    updateSquadMemberRole: vi.fn(),
    createAgent: vi.fn(),
    uploadFile: vi.fn(),
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: mocks.authUserId } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: vi.fn(), uploading: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "test-ws" }),
  useWorkspacePaths: () => ({
    squads: () => "/test-ws/squads",
    squadDetail: (id: string) => `/test-ws/squads/${id}`,
    agentDetail: (id: string) => `/test-ws/agents/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["workspaces", "ws-1", "agents"] }),
  memberListOptions: () => ({ queryKey: ["workspaces", "ws-1", "members"] }),
  workspaceKeys: {
    squads: (id: string) => ["workspaces", id, "squads"],
    agents: (id: string) => ["workspaces", id, "agents"],
  },
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@multica/core/utils", () => ({
  isImeComposing: () => false,
  timeAgo: () => "just now",
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/test-ws/squads/squad-1",
    push: vi.fn(),
  }),
  AppLink: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span />,
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => <span />,
}));

vi.mock("../../agents/components/create-agent-dialog", () => ({
  CreateAgentDialog: () => <div />,
}));

vi.mock("../../editor/content-editor", () => ({
  ContentEditor: () => <div />,
}));

vi.mock("../../editor/extensions/pinyin-match", () => ({
  matchesPinyin: () => false,
}));

vi.mock("../../issues/components/pickers/property-picker", () => ({
  PickerItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <div onClick={onClick}>{children}</div>
  ),
  PickerSection: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PickerEmpty: () => <div>No results</div>,
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({
    children,
    className,
    onClick,
    render,
  }: {
    children?: ReactNode;
    className?: string;
    onClick?: () => void;
    render?: ReactNode;
  }) => {
    if (render !== undefined) return <>{render}</>;
    return (
      <button type="button" className={className} onClick={onClick}>
        {children}
      </button>
    );
  },
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  AlertDialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogAction: ({
    children,
    onClick,
  }: {
    children: ReactNode;
    onClick?: () => void;
  }) => <button type="button" onClick={onClick}>{children}</button>,
  AlertDialogCancel: ({ children }: { children: ReactNode }) => (
    <button type="button">{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock("@multica/ui/components/ui/label", () => ({
  Label: ({ children, className }: { children: ReactNode; className?: string }) => (
    <label className={className}>{children}</label>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  TooltipTrigger: ({ render }: { render?: ReactNode; children?: ReactNode }) =>
    render !== undefined ? <>{render}</> : null,
  TooltipContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("../../layout/page-header", () => ({
  PageHeader: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { SquadDetailPage } from "./squad-detail-page";

const renderPage = () =>
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <SquadDetailPage />
    </I18nProvider>,
  );

describe("SquadDetailPage gating", () => {
  beforeEach(() => {
    mocks.authUserId = OWNER_USER;
  });

  it("shows the archive button for workspace owner viewing a member-created squad", () => {
    mocks.authUserId = OWNER_USER;
    renderPage();
    expect(screen.getByRole("button", { name: /archive/i })).toBeTruthy();
  });

  it("shows the archive button for the squad creator (a plain member)", () => {
    mocks.authUserId = CREATOR_USER;
    renderPage();
    expect(screen.getByRole("button", { name: /archive/i })).toBeTruthy();
  });

  it("hides the archive button for a plain member who is not the creator", () => {
    mocks.authUserId = BYSTANDER_USER;
    renderPage();
    expect(screen.queryByRole("button", { name: /archive/i })).toBeNull();
  });

  it("hides the add-member button for a plain member who is not the creator", () => {
    mocks.authUserId = BYSTANDER_USER;
    renderPage();
    expect(screen.queryByRole("button", { name: /add member/i })).toBeNull();
  });
});
