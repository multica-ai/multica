import type { SideWorkspaceControlRequest } from "../../../shared/side-workspace";
import { sideWorkspaceSyncSchema, type LocalConversationScope, type LocalPreviewLocation, type SideWorkspaceSnapshot } from "@multica/core/local-conversation/protocol";
import type { LocalV2Transport } from "@multica/core/local-conversation/v2";
import { conversationService } from "../platform/conversation-services";
import { localV2Preview } from "../platform/local-v2-preview";

export type SideTab = { id: string; kind: "side"; sessionId: string }
  | { id: string; kind: "file"; location: LocalPreviewLocation; selected?: LocalPreviewLocation; sourceId?: string; line?: number }
  | { id: string; kind: "web"; url: string; title?: string; loading?: boolean; openedBy?: "user" | "agent"; turnId?: string; keep?: boolean };
export type SideLayout = { tabs: SideTab[]; selected: string | null; open: boolean };
let revision = 0;
/** Where a conversation's Side layout is kept; another device's conversations are kept apart. */
export function sideLayoutKey(scope: LocalConversationScope, id: string, deviceId = "local") {
  return `multica-side-workspace:${scope.userId}:${scope.workspaceId}:${deviceId === "local" ? "" : `device:${deviceId}:`}session:${id}`;
}
export function sideSnapshot(conversationId: string, layout: SideLayout, visible: boolean): SideWorkspaceSnapshot {
  revision = Math.max(revision + 1, Date.now() * 1000);
  return sideWorkspaceSyncSchema.parse({ conversationId, revision, panelOpen: layout.open, visible, selectedTabId: layout.selected,
    tabs: layout.tabs.map((tab) => tab.kind === "web" ? tab : tab.kind === "side" ? tab : {
      id: tab.id, kind: "file", root: tab.location.root, path: tab.location.path, fileKind: tab.location.kind, selectedPath: tab.selected?.path, line: tab.line,
    }),
  });
}

const mountedScopes = new Map<string, number>();
const scopeKey = (scope: LocalConversationScope, deviceId: string) => JSON.stringify([deviceId, scope.userId, scope.workspaceId]);
export function registerSideWorkspaceView(scope: LocalConversationScope, deviceId = "local") {
  const key = scopeKey(scope, deviceId); mountedScopes.set(key, (mountedScopes.get(key) ?? 0) + 1);
  return () => { const count = (mountedScopes.get(key) ?? 1) - 1; if (count) mountedScopes.set(key, count); else mountedScopes.delete(key); };
}
export function hasSideWorkspaceView(scope: LocalConversationScope, deviceId = "local") { return mountedScopes.has(scopeKey(scope, deviceId)); }
export function readSideLayout(key: string): SideLayout {
  try {
    const value = JSON.parse(localStorage.getItem(key) || "null");
    if (!value || !Array.isArray(value.tabs)) throw new Error();
    const tabs = value.tabs.filter((tab: SideTab) => tab && typeof tab.id === "string" && (tab.kind === "side" ? typeof tab.sessionId === "string" : tab.kind === "web" ? typeof tab.url === "string" && /^https?:\/\//i.test(tab.url) : tab.kind === "file" && typeof tab.location?.root === "string" && typeof tab.location?.path === "string")).slice(0, 20);
    return { tabs, selected: tabs.some((tab: SideTab) => tab.id === value.selected) ? value.selected : tabs[0]?.id ?? null, open: value.open === true };
  } catch { return { tabs: [], selected: null, open: false }; }
}
/** Records a Side workspace layout in the daemon, where the conversation's tools read it. */
export async function syncSideWorkspace(transport: LocalV2Transport, scope: LocalConversationScope, snapshot: SideWorkspaceSnapshot) {
  const result = await conversationService(transport, scope).command(snapshot.conversationId, { type: "side_workspace_sync", workspace: snapshot }) as { revision?: unknown } | null;
  // The browser tabs follow the layout the daemon accepted.
  if (result?.revision === snapshot.revision) await window.desktopAPI.sideWorkspace?.reconcile(scope, snapshot.conversationId, (transport as { deviceId?: string }).deviceId ?? "local");
}
export async function prepareSideTab(request: SideWorkspaceControlRequest, transport: LocalV2Transport): Promise<SideTab | undefined> {
  if (request.op !== "open") return;
  if (!request.tab) throw new Error("Specify a tab");
  if (request.tab.kind === "web") {
    const url = new URL(request.tab.url);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("Use an HTTP or HTTPS address without credentials");
    return { id: `web:${crypto.randomUUID()}`, kind: "web", url: url.href, openedBy: "agent", turnId: request.turnId, keep: request.keep };
  }
  if (request.tab.kind === "side") {
    if (request.tab.sessionId === request.conversationId) throw new Error("Cannot open this conversation inside itself");
    await conversationService(transport, request.scope).snapshot(request.tab.sessionId);
    return { id: `side:${request.tab.sessionId}`, kind: "side", sessionId: request.tab.sessionId };
  }
  const file = await localV2Preview(transport, request.scope).locate(request.conversationId, request.tab.path);
  return { id: `file:${crypto.randomUUID()}`, kind: "file", sourceId: request.conversationId, location: { root: file.root, path: file.path, name: file.name, kind: file.kind } };
}
export function applySideCommand(previous: SideLayout, request: SideWorkspaceControlRequest, opened?: SideTab): { layout: SideLayout; tabId?: string } {
  let next = previous, tabId = request.tabId;
  if (opened) {
    const existing = previous.tabs.find((tab) => tab.id === opened.id || (tab.kind === "web" && opened.kind === "web" && tab.url === opened.url) || (tab.kind === "file" && opened.kind === "file" && tab.location.path === opened.location.path));
    if (!existing && previous.tabs.length >= 20) throw new Error("Side workspace supports at most 20 tabs");
    tabId = existing?.id ?? opened.id;
    next = { tabs: existing ? previous.tabs : [...previous.tabs, opened], selected: request.select === false ? previous.selected : tabId, open: request.select === false ? previous.open : true };
  } else if (request.op === "select") {
    if (!previous.tabs.some((tab) => tab.id === tabId)) throw new Error("not_in_current_side: Tab has closed");
    next = { ...previous, selected: tabId!, open: true };
  } else if (request.op === "release" || request.op === "close") {
    const tabs = previous.tabs.filter((tab) => {
      const remove = request.op === "release" ? tab.kind === "web" && tab.openedBy === "agent" && tab.turnId === request.turnId && !tab.keep : tab.id === tabId;
      if (remove && (tab.kind !== "web" || tab.openedBy !== "agent" || tab.turnId !== request.turnId)) throw new Error("User tabs cannot be closed by the assistant");
      return !remove;
    });
    next = { tabs, selected: tabs.some((tab) => tab.id === previous.selected) ? previous.selected : tabs.at(-1)?.id ?? null, open: tabs.length > 0 && previous.open };
  }
  return { layout: next, tabId };
}
