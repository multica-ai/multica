// @vitest-environment node
import type { ReactElement } from "react";
import { ActionSheetIOS } from "react-native";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ProjectsPage from "@/app/(app)/[workspace]/more/projects";
import NewProject from "@/app/(app)/[workspace]/project/new";
import ProjectDetail from "@/app/(app)/[workspace]/project/[id]";

const state = vi.hoisted(() => ({ allowed: false, create: vi.fn(), remove: vi.fn() }));
vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  useCallback: (callback: unknown) => callback,
  useMemo: (callback: () => unknown) => callback(),
}));
vi.mock("react-native", () => ({
  ActionSheetIOS: { showActionSheetWithOptions: vi.fn() },
  Alert: { alert: vi.fn() },
  ActivityIndicator: "ActivityIndicator", FlatList: "FlatList", View: "View",
  RefreshControl: "RefreshControl", ScrollView: "ScrollView",
}));
vi.mock("react-native-safe-area-context", () => ({ SafeAreaView: "SafeAreaView" }));
vi.mock("expo-router", () => ({
  Stack: { Screen: "Screen" }, router: { push: vi.fn(), back: vi.fn() },
  useLocalSearchParams: () => ({ id: "project-1" }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
  useQuery: (options: { queryKey: string[] }) => ({
    data: options.queryKey[0] === "detail" ? { id: "project-1", title: "Project" } : [],
    isLoading: false,
  }),
}));
vi.mock("@/lib/use-can-manage-projects", () => ({ useCanManageProjects: () => state.allowed }));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (s: { currentWorkspaceId: string; currentWorkspaceSlug: string }) => unknown) =>
    selector({ currentWorkspaceId: "workspace-1", currentWorkspaceSlug: "acme" }),
}));
vi.mock("@/data/auth-store", () => ({ useAuthStore: () => "user-1" }));
vi.mock("@/data/queries/projects", () => ({
  projectListOptions: () => ({ queryKey: ["list"] }),
  projectDetailOptions: () => ({ queryKey: ["detail"] }),
}));
vi.mock("@/data/queries/pins", () => ({ pinListOptions: () => ({ queryKey: ["pins"] }) }));
vi.mock("@/data/mutations/pins", () => ({ useCreatePin: () => ({}), useDeletePin: () => ({}) }));
vi.mock("@/data/mutations/projects", () => ({
  useCreateProject: state.create, useDeleteProject: () => ({ mutate: state.remove }),
}));
vi.mock("@/data/realtime/use-project-realtime", () => ({ useProjectRealtime: vi.fn() }));
vi.mock("@/data/stores/new-project-draft-store", () => ({ useNewProjectDraftStore: vi.fn() }));
vi.mock("@/lib/project-status", () => ({}));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("@/components/ui/button", () => ({ Button: "Button" }));
vi.mock("@/components/ui/icon-button", () => ({ IconButton: "IconButton" }));
vi.mock("@/components/ui/autosize-textarea", () => ({ AutosizeTextArea: "AutosizeTextArea" }));
vi.mock("@/components/ui/project-status-icon", () => ({ ProjectStatusIcon: "ProjectStatusIcon" }));
vi.mock("@/components/ui/project-priority-icon", () => ({ ProjectPriorityIcon: "ProjectPriorityIcon" }));
vi.mock("@/components/project/project-row", () => ({ ProjectRow: "ProjectRow" }));
vi.mock("@/components/project/project-header-card", () => ({ ProjectHeaderCard: "ProjectHeaderCard" }));
vi.mock("@/components/project/project-properties-section", () => ({ ProjectPropertiesSection: "ProjectPropertiesSection" }));
vi.mock("@/components/project/project-related-issues", () => ({ ProjectRelatedIssues: "ProjectRelatedIssues" }));
vi.mock("@/components/project/project-resources-section", () => ({ ProjectResourcesSection: "ProjectResourcesSection" }));

// Inspect JSX props and callbacks only; this is not native rendering coverage.
type Element = ReactElement<Record<string, any>>; // eslint-disable-line @typescript-eslint/no-explicit-any
function children(element: Element): Element[] {
  return element.props.children;
}

describe("project permission screen wiring", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.allowed = false;
  });

  it.each([false, true])("gates the projects header and empty-state buttons: %s", (allowed) => {
    state.allowed = allowed;
    const [header, empty] = children(ProjectsPage());
    expect(Boolean(header!.props.options.headerRight())).toBe(allowed);
    expect(Boolean(empty!.props.onCreate)).toBe(allowed);
    const emptyTree = (empty!.type as (props: unknown) => Element)(empty!.props);
    const action = children(emptyTree)[1]!;
    expect(action.type).toBe(allowed ? "Button" : "Text");
    if (!allowed) expect(action.props.children).toBe("permissions.create");
  });

  it("blocks a direct create route before mounting the form or its mutation", () => {
    const [header, message] = children(NewProject());
    expect(header!.props.options.headerRight()).toBeNull();
    expect(message!.props.children.props.children).toBe("permissions.create");
    expect(state.create).not.toHaveBeenCalled();
  });

  it("mounts the create form for a permitted user", () => {
    state.allowed = true;
    expect(typeof NewProject().type).toBe("function");
  });

  it.each([false, true])("offers Delete only to permitted users, without marking Edit destructive: %s", (allowed) => {
    state.allowed = allowed;
    const [header] = children(ProjectDetail());
    header!.props.options.headerRight().props.onPress();
    const [options] = vi.mocked(ActionSheetIOS.showActionSheetWithOptions).mock.lastCall!;
    expect(options.options.includes("common:actions.delete")).toBe(allowed);
    expect(options.options).toContain("menu.edit_details");
    if (allowed) {
      expect(options.destructiveButtonIndex).toBe(options.options.indexOf("common:actions.delete"));
    } else {
      expect(options.destructiveButtonIndex).toBeUndefined();
    }
    expect(state.remove).not.toHaveBeenCalled();
  });
});
