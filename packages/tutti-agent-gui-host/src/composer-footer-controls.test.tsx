// @vitest-environment jsdom
import { act, type ComponentProps } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ComposerFooterControls } from "./composer-footer-controls";
import { createMulticaAgentGuiRuntime as createRuntime } from "./runtime";
import type { AgentGuiInitSnapshot, AgentGuiRpcClient } from "./protocol";
import { localSourceFromClient } from "./runtime/local-snapshot-test-host";

const createMulticaAgentGuiRuntime = (client: AgentGuiRpcClient, init: AgentGuiInitSnapshot) => createRuntime(client, init, localSourceFromClient(client as never));

vi.mock("./usage-indicator", () => ({ UsageIndicator: () => null }));
vi.mock("./permission-picker", () => ({ PermissionPicker: () => null }));

let root: Root;
let container: HTMLDivElement;
let handle: ReturnType<typeof createMulticaAgentGuiRuntime>;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); handle?.dispose(); container.remove(); vi.unstubAllGlobals(); });

it("applies a new conversation directory only to its active home composer, without submitting or changing the draft", async () => {
  const target = { id: "local:codex", name: "Codex", provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true };
  const snapshot: AgentGuiInitSnapshot = { workspaceId: "ws", workspaceSlug: "workspace", userId: "user", theme: "dark", locale: "en", local: true,
    activeSessionId: null, selectedAgentId: target.id, agents: [], executionTargets: [target], sessions: [] };
  const request = vi.fn(async (method: string) => method === "workspace.load" ? snapshot : null);
  const client: AgentGuiRpcClient = { request: request as AgentGuiRpcClient["request"], subscribe: () => () => {} };
  handle = createMulticaAgentGuiRuntime(client, snapshot); await handle.initialize();
  const onProjectPathChange = vi.fn(); const onDraftContentChange = vi.fn(); const onReadyForFocus = vi.fn(); const onNewConversationApplied = vi.fn(); const onOpenNewConversation = vi.fn();
  const renderFooter = (sessionId: string | null, sequence: number, projectPath: string | null, active = true) => act(async () => root.render(<ComposerFooterControls
    context={{ agentSessionId: sessionId, isActive: active, selectedAgentTarget: { agentTargetId: target.id }, onProjectPathChange, onDraftContentChange } as unknown as ComponentProps<typeof ComposerFooterControls>["context"]}
    client={client} handle={handle} targets={[target]} snapshot={snapshot} newConversationRequest={{ sequence, projectPath }} onNewConversationApplied={onNewConversationApplied}
    onOpenNewConversation={onOpenNewConversation}
    onLeaveSession={vi.fn()} onCancelTargetSwitch={vi.fn()} onSelectAgent={vi.fn()} onReadyForFocus={onReadyForFocus} />));
  await renderFooter("outgoing-session", 1, "/project");
  expect(onProjectPathChange).not.toHaveBeenCalled();
  expect(onOpenNewConversation).toHaveBeenCalledOnce();
  await renderFooter(null, 1, "/project", false);
  expect(onProjectPathChange).not.toHaveBeenCalled();
  await renderFooter(null, 1, "/project");
  expect(onProjectPathChange.mock.calls).toEqual([["/project"]]);
  expect(onReadyForFocus).toHaveBeenCalledWith(1);
  await renderFooter(null, 1, "/project");
  expect(onProjectPathChange).toHaveBeenCalledOnce();
  await renderFooter(null, 2, null);
  expect(onProjectPathChange.mock.calls).toEqual([["/project"], [null]]);
  expect(onNewConversationApplied).toHaveBeenCalledTimes(2);
  expect(onDraftContentChange).not.toHaveBeenCalled();
  expect(request.mock.calls.some(([method]) => method === "session.create" || method === "session.send")).toBe(false);
});

it.each(["codex", "claude"] as const)("selects and displays %s thinking effort, then removes incompatible options on a model change", async (provider) => {
  const target = { id: `local:${provider}`, name: provider, provider, iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true };
  const snapshot: AgentGuiInitSnapshot = { workspaceId: "ws", workspaceSlug: "workspace", userId: "user", theme: "dark", locale: "en", local: true,
    activeSessionId: null, agents: [], executionTargets: [target], sessions: [] };
  const storage = new Map<string, string>();
  const request = vi.fn(async (method: string, params: Record<string, string>) => {
    if (method === "storage.read") return storage.get(params.key!) ?? null;
    if (method === "storage.write") { storage.set(params.key!, params.value!); return; }
    if (method === "workspace.load") return snapshot;
    if (method === "models.list") return { supported: true, models: [
      { id: "reasoning", label: "Reasoning", thinking: { supported_levels: [{ value: "low", label: "Low" }, { value: "high", label: "High" }] } },
      { id: "plain", label: "Plain" },
    ] };
    throw new Error(`Unexpected request: ${method}`);
  });
  const client: AgentGuiRpcClient = { request: request as AgentGuiRpcClient["request"], subscribe: () => () => {} };
  handle = createMulticaAgentGuiRuntime(client, snapshot); await handle.initialize();
  await handle.setModelSelection(target.id, null, "reasoning");
  await act(async () => root.render(<ComposerFooterControls context={{ selectedAgentTarget: { agentTargetId: target.id } } as ComponentProps<typeof ComposerFooterControls>["context"]}
    client={client} handle={handle} targets={[target]} snapshot={snapshot} onLeaveSession={vi.fn()} onCancelTargetSwitch={vi.fn()} onSelectAgent={vi.fn()} onReadyForFocus={vi.fn()} />));
  const open = () => act(async () => {
    container.querySelector('[data-testid="multica-model-picker"]')!.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
  });
  const option = (label: string) => [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"]')].find((entry) => entry.textContent === label)!;
  const openEffort = () => act(async () => {
    document.querySelector('.multica-model-picker-effort')!.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));
  });
  await open();
  expect(document.body.textContent).toContain("Thinking effort");
  await openEffort();
  await act(async () => option("High").click());
  expect(handle.getReasoningSelection(target.id)).toBe("high");
  expect(container.querySelector('[data-testid="multica-model-picker"]')?.textContent).toContain("High");
  await open();
  await openEffort();
  expect(option("High").getAttribute("aria-checked")).toBe("true");
  await act(async () => option("Plain").click());
  expect(handle.getReasoningSelection(target.id)).toBeNull();
  await open();
  expect(document.body.textContent).toContain("No effort options reported for this model");
  expect(option("High")).toBeUndefined();
});

it.each([{ locale: "en" }, { locale: "zh-CN" }] as const)("keeps error recovery and scopes the model menu footer for locale=$locale", async ({ locale }) => {
  const target = { id: "local:codex", name: "Codex", provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true };
  const snapshot: AgentGuiInitSnapshot = { workspaceId: "ws", workspaceSlug: "workspace", userId: "user", theme: "light", locale, local: true,
    activeSessionId: null, agents: [], executionTargets: [target], sessions: [] };
  let attempts = 0;
  const request = vi.fn(async (method: string) => {
    if (method === "storage.read") return null;
    if (method === "storage.write") return;
    if (method === "workspace.load") return snapshot;
    if (method === "models.list") {
      if (++attempts === 1) throw new Error("Catalog unavailable");
      return { supported: true, models: [{ id: "reasoning", label: "Reasoning", thinking: { supported_levels: [{ value: "high", label: "High" }] } }] };
    }
    throw new Error(`Unexpected request: ${method}`);
  });
  const client: AgentGuiRpcClient = { request: request as AgentGuiRpcClient["request"], subscribe: () => () => {} };
  handle = createMulticaAgentGuiRuntime(client, snapshot); await handle.initialize();
  await act(async () => root.render(<ComposerFooterControls context={{ selectedAgentTarget: { agentTargetId: target.id } } as ComponentProps<typeof ComposerFooterControls>["context"]}
    client={client} handle={handle} targets={[target]} snapshot={snapshot} onLeaveSession={vi.fn()} onCancelTargetSwitch={vi.fn()} onSelectAgent={vi.fn()} onReadyForFocus={vi.fn()} />));
  const open = () => act(async () => {
    container.querySelector('[data-testid="multica-model-picker"]')!.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
  });
  const item = (label: string) => [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((entry) => entry.textContent === label);
  const retry = locale === "zh-CN" ? "重试" : "Retry";
  const refresh = locale === "zh-CN" ? "刷新模型" : "Refresh models";
  const scope = locale === "zh-CN" ? "用于当前会话的运行器" : "Applies to this conversation's provider";
  await open();
  expect(document.querySelector('[role="alert"]')?.textContent).toContain("Catalog unavailable");
  expect(item(retry)).toBeDefined();
  await act(async () => item(retry)!.click());
  expect(request).toHaveBeenCalledWith("models.list", { agentId: target.id, force: true }, expect.anything());
  expect(document.querySelector('[role="alert"]')).toBeNull();
  expect(item(retry)).toBeUndefined();
  expect(item(refresh)).toBeUndefined();
  expect(document.body.textContent).not.toContain(scope);
  expect(document.querySelector('.multica-model-picker-menu')!.lastElementChild?.getAttribute("role")).not.toBe("separator");
  const model = [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"]')].find((entry) => entry.textContent === "Reasoning")!;
  await act(async () => model.click());
  expect(handle.getModelSelection(target.id)).toBe("reasoning");
  await open();
  expect(attempts).toBe(2);
  expect(document.querySelector('.multica-model-picker-effort')).not.toBeNull();
});
