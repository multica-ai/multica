// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, AgentRuntime, MemberWithUser } from "@multica/core/types";
import type { AgentRuntimePreference } from "@multica/core/types";
import { api } from "@multica/core/api";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { PersonalRuntimeSettings } from "./personal-runtime-settings";

vi.mock("@multica/core/api", () => ({ api: {
  getAgentRuntimePreference: vi.fn(), updateAgentRuntimePreference: vi.fn(), updateAgent: vi.fn(),
} }));
const authUser = { id: "me" };
vi.mock("@multica/core/auth", () => ({ useAuthStore: { getState: () => ({ user: authUser }) } }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../runtimes/components/provider-logo", () => ({ ProviderLogo: () => null }));
const agent = { id: "agent", workspace_id: "ws", runtime_id: "default", owner_id: "other", permission_mode: "public_to", invocation_targets: [{ target_type: "workspace", target_id: "ws" }] } as Agent;
const members = [{ user_id: "me", role: "member", name: "Me" }] as MemberWithUser[];
const runtime = (id: string, owner = "me", provider = "codex") => ({ id, name: `${provider} (my-machine)`, custom_name: id === "mine" ? "My laptop" : "Shared machine", daemon_id: id, workspace_id: "ws", owner_id: owner, provider, runtime_mode: "local", status: "online", visibility: "private", device_info: id, metadata: {} }) as AgentRuntime;
const runtimes = [runtime("default", "other"), runtime("mine"), runtime("another-provider", "me", "claude"), runtime("other-public", "other")];
function mount(a = agent, availableRuntimes = runtimes) {
 const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
 return render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { agents: enAgents, common: enCommon, issues: enIssues } }}><PersonalRuntimeSettings agent={a} runtimes={availableRuntimes} members={members} currentUserId="me" /></I18nProvider></QueryClientProvider>);
}
beforeEach(() => { vi.clearAllMocks(); vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: null }); vi.mocked(api.updateAgentRuntimePreference).mockResolvedValue({ runtimeId: "mine" }); });
afterEach(cleanup);
describe("personal runtime settings", () => {
 it.each(["claude", "codex", "gemini"])("selects an owned %s runtime when the default machine is hidden", async (provider) => {
  vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: null, provider });
  mount(agent, [runtime("mine", "me", provider), runtime("other-public", "other", provider)]);
  fireEvent.click(await screen.findByRole("button", { name: "Use agent default" }));
  fireEvent.click(screen.getByRole("button", { name: new RegExp(provider, "i") }));
  await waitFor(() => expect(api.updateAgentRuntimePreference).toHaveBeenCalledWith("agent", "ws", "mine"));
 });

 it("allows an invocable non-owner to select their matching provider without changing the shared agent", async () => {
  mount(); await waitFor(() => expect(screen.getByRole("button", { name: "Use agent default" })).toBeTruthy());
  fireEvent.click(screen.getByRole("button", { name: "Use agent default" }));
  fireEvent.click(screen.getByRole("button", { name: /My laptop/i }));
  fireEvent.click(screen.getByRole("button", { name: /codex/i }));
  await waitFor(() => expect(api.updateAgentRuntimePreference).toHaveBeenCalledWith("agent", "ws", "mine"));
  expect(api.updateAgent).not.toHaveBeenCalled();
  expect(screen.queryByText("other-public")).toBeNull();
 });
 it("keeps an unavailable stored choice explicit and allows reset", async () => {
  vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: "deleted" });
  mount(); await screen.findByText("Your selected runtime is unavailable. Choose another runtime or reset to the agent default.");
  fireEvent.click(screen.getByRole("button", { name: /Runtime · none selected/ }));
  fireEvent.click(screen.getByRole("button", { name: "Use agent default" }));
  await waitFor(() => expect(api.updateAgentRuntimePreference).toHaveBeenCalledWith("agent", "ws", null));
 });
 it("does not fetch or expose editing for a private agent the member cannot invoke", () => {
  mount({ ...agent, permission_mode: "private", invocation_targets: [] });
  expect(api.getAgentRuntimePreference).not.toHaveBeenCalled();
  expect(screen.queryByText("My execution runtime")).toBeNull();
 });
 it("does not label malformed preference data as default", async () => {
  vi.mocked(api.getAgentRuntimePreference).mockResolvedValue(null);
  mount(); await screen.findByText("Unable to load your runtime preference.");
  expect(screen.queryByRole("button", { name: "Use agent default" })).toBeNull();
 });
});

it("saves personal model and concurrency without editing the shared agent", async () => {
 vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({runtimeId:"mine",modelMode:"inherit",maxConcurrentTasks:null});
 mount();
 await userEvent.click(await screen.findByLabelText("Model selection"));
 await userEvent.click(await screen.findByRole("option", { name: "Choose a model" }));
 fireEvent.change(screen.getByLabelText("Model ID"),{target:{value:"my-model"}});
 fireEvent.change(screen.getByLabelText("My concurrency limit"),{target:{value:"3"}});
 fireEvent.click(screen.getByRole("button",{name:"Save execution settings"}));
 await waitFor(()=>expect(api.updateAgentRuntimePreference).toHaveBeenCalledWith("agent","ws",{runtimeId:"mine",modelMode:"custom",model:"my-model",maxConcurrentTasks:3}));
 expect(api.updateAgent).not.toHaveBeenCalled();
});

it("uses the shared configuration for owners unless they explicitly expand personal settings", async () => {
 mount({ ...agent, owner_id: "me" });
 fireEvent.click(await screen.findByRole("button", { name: "Configure separately for myself" }));
 expect(screen.getByRole("button", { name: "Use agent default" })).toBeTruthy();
 expect(api.updateAgentRuntimePreference).not.toHaveBeenCalled();
});

it("does not show a second runtime picker to owners without an override", async () => {
 mount({ ...agent, owner_id: "me" });
 await screen.findByRole("button", { name: "Configure separately for myself" });
 expect(screen.queryByRole("button", { name: "Use agent default" })).toBeNull();
 expect(screen.queryByText("My execution settings")).toBeNull();
});

it("keeps an owner's unavailable override visible until explicitly reset", async () => {
 vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: "deleted" });
 mount({ ...agent, owner_id: "me" });
 await screen.findByText("Currently using a personal override");
 expect(api.updateAgentRuntimePreference).not.toHaveBeenCalled();
 vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: null });
 vi.mocked(api.updateAgentRuntimePreference).mockResolvedValue({ runtimeId: null });
 fireEvent.click(screen.getByRole("button", { name: "Restore default configuration" }));
 await screen.findByRole("button", { name: "Configure separately for myself" });
 expect(api.updateAgentRuntimePreference).toHaveBeenCalledWith("agent", "ws", null);
 expect(api.updateAgent).not.toHaveBeenCalled();
});

it("does not conceal an owner's override after a failed reset", async () => {
 vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: "mine" });
 vi.mocked(api.updateAgentRuntimePreference).mockRejectedValue(new Error("offline"));
 mount({ ...agent, owner_id: "me" });
 fireEvent.click(await screen.findByRole("button", { name: "Restore default configuration" }));
 await screen.findByText("Unable to save your runtime preference.");
 expect(screen.getByText("Currently using a personal override")).toBeTruthy();
});

it("restores saved personal execution settings after a fresh query client mounts", async () => {
 let persisted: AgentRuntimePreference = { runtimeId: "mine", modelMode: "inherit", maxConcurrentTasks: null };
 vi.mocked(api.getAgentRuntimePreference).mockImplementation(async () => persisted);
 vi.mocked(api.updateAgentRuntimePreference).mockImplementation(async (_agentId, _wsId, value) => {
  if (typeof value === "object" && value !== null) persisted = value;
  return persisted;
 });
 const first = mount();
 await userEvent.click(await screen.findByLabelText("Model selection"));
 await userEvent.click(await screen.findByRole("option", { name: "Choose a model" }));
 fireEvent.change(screen.getByLabelText("Model ID"), { target: { value: "saved-model" } });
 fireEvent.change(screen.getByLabelText("My concurrency limit"), { target: { value: "4" } });
 fireEvent.click(screen.getByRole("button", { name: "Save execution settings" }));
 await waitFor(() => expect(persisted).toMatchObject({ model: "saved-model", maxConcurrentTasks: 4 }));
 first.unmount();
 mount();
 expect(await screen.findByLabelText("Model ID")).toHaveValue("saved-model");
 expect(screen.getByLabelText("Model selection")).toHaveTextContent("Choose a model");
 expect(screen.getByLabelText("My concurrency limit")).toHaveValue(4);
 expect(api.updateAgent).not.toHaveBeenCalled();
});

it.each(["claude", "codex", "gemini"])("offers owned %s alongside all providers on the same machine", async (provider) => {
 vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: null, provider: "claude" });
 const available = ["claude", "codex", "gemini"].map((name) => ({ ...runtime(name, "me", name), daemon_id: "my-mac", custom_name: "My Mac" }));
 mount(agent, available);
 fireEvent.click(await screen.findByRole("button", { name: "Use agent default" }));
 for (const name of ["claude", "codex", "gemini"]) expect(screen.getByRole("button", { name: new RegExp(name, "i") })).toBeTruthy();
 fireEvent.click(screen.getByRole("button", { name: new RegExp(provider, "i") }));
 await waitFor(() => expect(api.updateAgentRuntimePreference).toHaveBeenCalledWith("agent", "ws", provider));
});

it("offers only runtime default or custom models even for a matching provider", async () => {
 vi.mocked(api.getAgentRuntimePreference).mockResolvedValue({ runtimeId: "mine", provider: "codex", modelMode: "inherit" });
 mount();
 expect(await screen.findByLabelText("Model selection")).toHaveTextContent("Use runtime default model");
 fireEvent.click(screen.getByLabelText("Model selection"));
 expect(screen.queryByRole("option", { name: "Use agent model" })).toBeNull();
 expect(screen.getByRole("option", { name: "Choose a model" })).toBeTruthy();
});
