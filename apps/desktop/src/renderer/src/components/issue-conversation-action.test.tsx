import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import issues from "../../../../../../packages/views/locales/en/issues.json";
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent } from "@multica/ui/components/ui/dropdown-menu";
import { DesktopIssueConversationAction } from "./issue-conversation-action";
import { useIssueConversationStore } from "../stores/issue-conversation-store";

const mock = vi.hoisted(() => ({ devices: [] as { peer_id: string; device_name: string }[], devicesPending: false, devicesError: false, transport: vi.fn(), request: vi.fn(), open: vi.fn(), replace: vi.fn(), error: vi.fn(), tab: { id: "issue-tab", url: "/test/issues/CHOV-85" } }));
vi.mock("@multica/core/api", () => ({ api: { getBaseUrl: () => "https://api.test" } }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: Object.assign((select: (s: unknown) => unknown) => select({ user: { id: "user" } }), { getState: () => ({ user: { id: "user" } }) }) }));
vi.mock("@multica/core/platform", () => ({ getCurrentWsId: () => "workspace" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ conversationFlow: () => "/test/conversations", conversationFlowDevice: (id: string) => `/test/conversations/devices/${id}` }) }));
vi.mock("@multica/views/navigation", () => ({ useNavigation: () => ({ openInNewTab: mock.open, replace: mock.replace }) }));
vi.mock("@multica/views/i18n", () => ({ useT: () => ({ t: (select: (data: typeof issues) => string, values?: { message: string }) => select(issues).replace("{{message}}", values?.message ?? "") }) }));
vi.mock("../platform/linked-devices", () => ({ useLinkedDevicesQuery: () => ({ data: mock.devices, isPending: mock.devicesPending, isError: mock.devicesError }), deviceName: (devices: typeof mock.devices, id: string) => devices.find((d) => d.peer_id === id)?.device_name }));
vi.mock("../platform/local-v2-transport", () => ({ localV2Transport: mock.transport }));
vi.mock("../platform/conversation-services", () => ({ conversationService: () => ({ request: mock.request, receiveSummary: vi.fn() }) }));
vi.mock("../stores/tab-store", () => ({ useTabStore: { getState: () => ({}) }, getActiveTab: () => mock.tab }));
vi.mock("sonner", () => ({ toast: { error: mock.error } }));

const issue = { id: "issue-uuid", identifier: "CHOV-85", workspace_id: "workspace" } as Issue;
const summary = { conversation_id: "created", summary_seq: 1, provider: "codex", title: "CHOV-85", status: "idle", pinned: false, archived: false, updated_at: "now" };
beforeEach(() => {
  vi.clearAllMocks();
  mock.devices = [];
  mock.devicesPending = false;
  mock.devicesError = false;
  mock.request.mockImplementation(async (method: string) => method === "GET" ? { providers: [{ id: "codex", available: true }] } : summary);
  useIssueConversationStore.setState({ drafts: {}, pending: {} });
});
async function menu() {
  render(<DropdownMenu><DropdownMenuTrigger render={<button>Menu</button>} /><DropdownMenuContent><DesktopIssueConversationAction issue={issue} /></DropdownMenuContent></DropdownMenu>);
  fireEvent.click(screen.getByText("Menu"));
  return screen.findByRole("menuitem", { name: "Go to AI conversation" });
}

it("offers a direct action when only this device is available", async () => {
  const action = await menu();
  expect(action).not.toHaveAttribute("aria-haspopup");
  fireEvent.click(action);
  await waitFor(() => expect(mock.open).toHaveBeenCalledWith("/test/conversations?session=created&provider=codex", undefined, { activate: true }));
  expect(mock.transport).toHaveBeenCalledWith("local");
  expect(mock.replace).toHaveBeenCalledWith("/test/conversations?session=created&provider=codex");
});

it("lists local and linked devices and creates on the selected device", async () => {
  mock.devices = [{ peer_id: "peer-1", device_name: "Mac Studio" }];
  const action = await menu();
  expect(action).toHaveAttribute("aria-haspopup", "menu");
  fireEvent.keyDown(action, { key: "ArrowRight" });
  expect(await screen.findByRole("menuitem", { name: "Local" })).toBeVisible();
  fireEvent.click(await screen.findByRole("menuitem", { name: "Mac Studio" }));
  await waitFor(() => expect(mock.open).toHaveBeenCalledWith("/test/conversations/devices/peer-1?session=created&provider=codex", undefined, { activate: true }));
  expect(mock.transport).toHaveBeenCalledWith("peer-1");
});

it("shows a creation error without navigating", async () => {
  mock.request.mockRejectedValueOnce(new Error("device_link_off"));
  fireEvent.click(await menu());
  await waitFor(() => expect(mock.error).toHaveBeenCalledWith(expect.stringContaining("device_link_off")));
  expect(mock.open).not.toHaveBeenCalled();
  expect(mock.replace).not.toHaveBeenCalled();
});

it.each(["loading", "failed"])("waits for device discovery when it is %s instead of assuming local", async (state) => {
  mock.devicesPending = state === "loading";
  mock.devicesError = state === "failed";
  const action = await menu();
  expect(action).toHaveAttribute("aria-disabled", "true");
  fireEvent.click(action);
  expect(mock.request).not.toHaveBeenCalled();
  if (state === "failed") expect(action).toHaveAttribute("title", issues.actions.conversation_devices_failed);
});
