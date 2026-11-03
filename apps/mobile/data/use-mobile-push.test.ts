// @vitest-environment node
import { beforeEach, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  setup: null as null | (() => (() => void) | undefined),
  response: null as null | ((value: unknown) => void),
  onState: null as null | ((value: string) => void),
  last: null as unknown,
  push: vi.fn(), listWorkspaces: vi.fn(), register: vi.fn(), clear: vi.fn(),
}));
vi.mock("react", () => ({ useEffect: (setup: () => (() => void) | undefined) => { state.setup = setup; } }));
vi.mock("react-native", () => ({ AppState: { addEventListener: (_: string, callback: (value: string) => void) => { state.onState = callback; return {remove: vi.fn()}; } } }));
vi.mock("expo-router", () => ({ router: { push: state.push } }));
vi.mock("expo-notifications", () => ({
  DEFAULT_ACTION_IDENTIFIER: "default",
  getLastNotificationResponseAsync: async () => state.last,
  clearLastNotificationResponseAsync: state.clear,
  addNotificationResponseReceivedListener: (callback: (value: unknown) => void) => { state.response = callback; return {remove: vi.fn()}; },
  addPushTokenListener: () => ({remove: vi.fn()}),
}));
vi.mock("./mobile-push", () => ({ registerMobilePush: state.register, pushWorkspace: (data: {workspace: string}) => data.workspace }));
vi.mock("./api", () => ({ api: { listWorkspaces: state.listWorkspaces } }));
import { useMobilePush } from "./use-mobile-push";
const response = { actionIdentifier: "default", notification: { request: { identifier: "notification", content: { data: { workspace: "team" } } } } };

beforeEach(() => {
  vi.clearAllMocks();state.last=null;state.response=null;
  state.listWorkspaces.mockResolvedValue([{slug:"team"}]);
});

it("handles a cold notification once, checks membership and refreshes on foreground", async () => {
  state.last=response;useMobilePush("user");const cleanup=state.setup!();
  await vi.waitFor(() => expect(state.push).toHaveBeenCalledTimes(1));
  expect(state.push).toHaveBeenCalledWith({pathname:"/[workspace]/(tabs)/inbox",params:{workspace:"team"}});
  state.response!(response);
  await Promise.resolve();
  expect(state.push).toHaveBeenCalledTimes(1);
  state.onState!("active");expect(state.register).toHaveBeenCalledTimes(2);
  cleanup?.();
});
it("does not open a workspace the current account cannot access", async () => {
  state.listWorkspaces.mockResolvedValue([]);state.last=response;
  useMobilePush("other");const cleanup=state.setup!();
  await vi.waitFor(() => expect(state.listWorkspaces).toHaveBeenCalled());
  expect(state.push).not.toHaveBeenCalled();cleanup?.();
});
it("drops a notification lookup that finishes after sign-out", async () => {
  let finish!: (value: {slug:string}[]) => void;
  state.listWorkspaces.mockImplementationOnce(() => new Promise((resolve) => { finish=resolve; }));
  state.last=response;useMobilePush("user");const cleanup=state.setup!();
  await vi.waitFor(() => expect(state.listWorkspaces).toHaveBeenCalled());
  cleanup?.();finish([{slug:"team"}]);await Promise.resolve();
  expect(state.push).not.toHaveBeenCalled();
});
