// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import type { AgentGUIProps } from "@tutti-os/agent-gui/agent-gui";
import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";
import { normalizeAgentGUIAgents, projectAgentGUIAgentsToTargets } from "../../../vendor/tutti/packages/agent/gui/agents";
import { AgentTargetPresentationProvider } from "../../../vendor/tutti/packages/agent/gui/shared/AgentTargetPresentationContext";
import { AgentRichTextReadonly } from "../../../vendor/tutti/packages/agent/gui/shared/AgentRichTextReadonly";
import { AgentGUIMentionServiceBoundary } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/AgentGUIMentionServiceBoundary";
import { projectMentionAgentTargetPresentations } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentGuiTargetPresentation";
import { AgentGuiApplication } from "./application";
import type { AgentGuiInitSnapshot, AgentGuiRpcClient, AgentGuiWorkspaceState } from "./protocol";

const captured = vi.hoisted(() => ({ props: null as AgentGUIProps | null, content: "" }));

// Keep the host's directory wiring and the real user-message rendering path,
// without starting the unrelated conversation runtime or provider UI.
vi.mock("@tutti-os/agent-gui/agent-gui", () => ({
  AgentGUI: (props: AgentGUIProps) => {
    captured.props = props;
    const targets = (directory: AgentGUIProps["agentDirectory"]) => projectAgentGUIAgentsToTargets(normalizeAgentGUIAgents(directory.agents));
    const presentations = projectMentionAgentTargetPresentations({
      railTargets: targets(props.agentDirectory),
      mentionTargets: props.mentionAgentDirectory ? targets(props.mentionAgentDirectory) : undefined,
      ownerSeparator: " · ",
      workspaceId: props.identity.workspaceId,
    });
    return <AgentGUIMentionServiceBoundary service={props.hostCapabilities.mentionService}>
      <AgentTargetPresentationProvider agentTargets={presentations}>
        <AgentRichTextReadonly value={captured.content} documentCacheKey="user-message" />
      </AgentTargetPresentationProvider>
    </AgentGUIMentionServiceBoundary>;
  },
}));

const hunterId = "11111111-1111-4111-8111-111111111111";
const reviewerId = "22222222-2222-4222-8222-222222222222";
const image = "https://example.test/hunter.png";
const emoji = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg"><text>🕵️</text></svg>')}#multica-agent-avatar`;

afterEach(() => { captured.props = null; captured.content = ""; vi.unstubAllGlobals(); });

async function mount(local = true, options: { activeSessionId?: string; continuable?: boolean } = {}) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let workspace = {
    local,
    agents: [
      { agent: { id: hunterId, name: "Multica 捉虫高手" }, provider: "codex", iconUrl: image, canInvoke: true, runtimeBound: true, runtimeOnline: true },
      { agent: { id: reviewerId, name: "Reviewer" }, provider: "codex", iconUrl: emoji, canInvoke: false, runtimeBound: false, runtimeOnline: false, unavailableReason: "Offline" },
    ],
    ...(local ? { executionTargets: [
      { id: "local:codex", name: "Codex", provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true },
      { id: "local:claude", name: "Claude Code", provider: "claude", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true },
    ] } : {}),
    sessions: [],
  } as unknown as AgentGuiWorkspaceState;
  const initial: AgentGuiInitSnapshot = { ...workspace, workspaceId: "ws", workspaceSlug: "workspace", userId: "user", locale: "en", theme: "dark", activeSessionId: options.activeSessionId ?? null };
  const listeners = new Set<() => void>();
  const handle = {
    getWorkspaceState: () => workspace,
    subscribeWorkspace: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    subscribeTurns: () => () => {},
    continuable: () => options.continuable ?? false,
    continueSession: vi.fn(),
    toGuiSessionId: (id: string) => id,
    toServerSessionId: (id: string) => id,
    runtime: {},
  } as unknown as Parameters<typeof AgentGuiApplication>[0]["handle"];
  const client = { request: vi.fn().mockResolvedValue({}), subscribe: () => () => {} } as unknown as AgentGuiRpcClient;
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  const container = document.createElement("div"); document.body.append(container);
  const root = createRoot(container);
  const render = () => root.render(<AgentGuiApplication client={client} initial={initial} handle={handle} root={container} />);
  await act(async () => render());
  return {
    container, client, handle,
    props: () => captured.props!,
    render: async () => { await act(async () => render()); },
    update: async (agents: AgentGuiWorkspaceState["agents"]) => {
      await act(async () => { workspace = { ...workspace, agents }; for (const listener of listeners) listener(); });
    },
    reload: async () => { await act(async () => root.render(null)); await act(async () => render()); },
    cleanup: async () => { await act(async () => root.unmount()); container.remove(); expect(listeners.size).toBe(0); },
  };
}

it("renders current workspace avatars in sent and reloaded user messages without changing local execution targets", async () => {
  const app = await mount();
  const mentions = () => Array.from(app.container.querySelectorAll<HTMLElement>('.agent-rich-text-mention-node[data-agent-file-mention="true"]'));
  const icons = () => mentions().map((mention) => mention.querySelector("img")?.getAttribute("src"));
  try {
    const provider = app.props().hostCapabilities.mentionService!.getProvider("agent-target")!;
    const items = await provider.query({ keyword: "", trigger: "@", context: {} });
    captured.content = items.map((item) => {
      const inserted = provider.toInsertResult(item);
      if (inserted.kind !== "mention") throw new Error("Expected mention");
      return createRichTextMentionMarkdown({ providerId: "agent-target", ...inserted.mention });
    }).join(" ");
    expect(captured.content).not.toContain(image);
    await app.render();
    expect(icons()).toEqual([image, emoji]);
    expect(app.props().agentDirectory.agents.map((entry) => entry.agentTargetId)).toEqual(["local:codex", "local:claude"]);
    expect(app.props().state.agentTargetId).toBe("local:codex");
    expect(app.props().mentionAgentDirectory?.agents.find((entry) => entry.agentTargetId === reviewerId)?.availability.status).toBe("unavailable");
    const href = mentions()[0]!.getAttribute("data-agent-mention-href")!;
    await act(async () => mentions()[0]!.click());
    expect(app.client.request).toHaveBeenCalledWith("host.openInternalResource", { href, background: false });
    await app.reload();
    expect(icons()).toEqual([image, emoji]);
    const agents = app.props().mentionAgentDirectory!.agents.filter((entry) => [hunterId, reviewerId].includes(entry.agentTargetId));
    await app.update(agents.map((entry) => ({
      agent: { id: entry.agentTargetId, name: entry.agentTargetId === hunterId ? "Renamed hunter" : entry.name },
      provider: entry.provider, iconUrl: entry.agentTargetId === hunterId ? "https://example.test/new.png" : "",
      canInvoke: false, runtimeBound: false, runtimeOnline: false,
    })) as AgentGuiWorkspaceState["agents"]);
    expect(icons()[0]).toBe("https://example.test/new.png");
    expect(icons()[1]).toContain("agent-colorful.png");
    expect(mentions()[0]!.textContent).toContain("Renamed hunter");
    expect(app.props().state.agentTargetId).toBe("local:codex");
    await act(async () => app.props().hostActions.onUpdateNode((current) => ({ ...current, provider: "claude", agentTargetId: "local:claude" })));
    expect(app.client.request).toHaveBeenCalledWith("host.selectSession", { sessionId: null, agentId: "local:claude" });
  } finally { await app.cleanup(); }
});

it("keeps one presentation per workspace identity when cloud execution and mention directories overlap", async () => {
  const app = await mount(false);
  try {
    expect(app.props().mentionAgentDirectory?.agents.map((entry) => entry.agentTargetId)).toEqual([hunterId, reviewerId]);
    expect(app.props().agentDirectory.agents.map((entry) => entry.agentTargetId)).toEqual([hunterId, reviewerId]);
    expect(app.props().mentionAgentDirectory?.agents.map((entry) => entry.iconUrl)).toEqual([image, emoji]);
  } finally { await app.cleanup(); }
});

it("turns the empty send button into Continue only for a continuable local turn", async () => {
  const stopped = await mount(true, { activeSessionId: "s1", continuable: true });
  try {
    const action = stopped.props().renderSlots?.composerEmptySubmit;
    expect(action?.label).toBe("Continue");
    action!.onSubmit();
    expect(stopped.handle.continueSession).toHaveBeenCalledWith("s1");
  } finally { await stopped.cleanup(); }
  const settled = await mount(true, { activeSessionId: "s1", continuable: false });
  try { expect(settled.props().renderSlots?.composerEmptySubmit).toBeNull(); } finally { await settled.cleanup(); }
});
