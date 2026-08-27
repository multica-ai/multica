import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Issue, IssueAssigneeType } from "@multica/core/types";
import { AppLink, NavigationProvider, type NavigationAdapter } from "../../navigation";
import {
  IssueSurfaceActionsProvider,
  type IssueSurfaceActions,
} from "../surface/actions-context";
import { BoardCardContent } from "./board-card";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/properties", () => ({
  propertyListOptions: () => ({ queryKey: ["properties"] }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "viewer-1" } }),
}));

vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: () => true,
  useAgentPresenceDetail: () => ({ availability: "offline", workload: null }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
  useWorkspacePaths: () => ({
    memberDetail: (id: string) => `/acme/members/${id}`,
    agentDetail: (id: string) => `/acme/agents/${id}`,
    squadDetail: (id: string) => `/acme/squads/${id}`,
  }),
}));

const viewState = vi.hoisted(() => ({
  cardProperties: {
    priority: false,
    description: false,
    assignee: true,
    startDate: true,
    dueDate: true,
    project: false,
    childProgress: false,
    labels: false,
  },
  cardPropertyIds: [],
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: typeof viewState) => unknown) => selector(viewState),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string) => `Assigned ${type}`,
    getActorInitials: () => "AA",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "Translated" }),
  useTimeAgo: () => () => "now",
}));

vi.mock("../../agents/components/agent-profile-card", () => ({
  AgentProfileCard: () => null,
}));

vi.mock("../../agents/components/agent-live-peek-card", () => ({
  AgentLivePeekCard: () => null,
}));

vi.mock("../../members/member-profile-card", () => ({
  MemberProfileCard: () => null,
}));

vi.mock("../../squads/components/squad-profile-card", () => ({
  SquadProfileCard: () => null,
}));

vi.mock("./issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: () => null,
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path) => `https://app.example${path}`,
};

const actions: IssueSurfaceActions = {
  isPending: false,
  createIssue: vi.fn(),
  updateIssue: vi.fn(),
  moveIssue: vi.fn(),
  batchUpdate: vi.fn().mockResolvedValue(undefined),
  batchDelete: vi.fn().mockResolvedValue(undefined),
};

function makeIssue(assigneeType: IssueAssigneeType): Issue {
  return {
    id: `issue-${assigneeType}`,
    workspace_id: "ws-1",
    number: 6082,
    identifier: "MUL-6082",
    title: "Fix Board assignee interaction",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: assigneeType,
    assignee_id: `${assigneeType}-1`,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: "2026-08-12",
    due_date: "2026-08-13",
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2026-08-12T00:00:00Z",
    updated_at: "2026-08-12T00:00:00Z",
  };
}

describe("BoardCardContent assignee picker", () => {
  it.each<IssueAssigneeType>(["member", "agent", "squad"])(
    "opens the picker from an avatar-only %s assignee without navigating the card",
    (assigneeType) => {
      const issue = makeIssue(assigneeType);
      const { container } = render(
        <NavigationProvider value={navigation}>
          <IssueSurfaceActionsProvider actions={actions}>
            <AppLink href={`/acme/issues/${issue.id}`}>
              <BoardCardContent issue={issue} editable />
            </AppLink>
          </IssueSurfaceActionsProvider>
        </NavigationProvider>,
      );
      const avatar = container.querySelector('[data-slot="avatar"]');

      expect(avatar).not.toBeNull();
      expect(avatar!.closest('[role="link"]')).toBeNull();
      expect(fireEvent.click(avatar!)).toBe(false);
      expect(screen.getByRole("textbox")).toBeInTheDocument();
      expect(navigation.push).not.toHaveBeenCalled();
    },
  );
});
