// Test rig: real list row, read service and viewport hook with a fake authoritative daemon.
import { useRef, useState, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { observer } from "mobx-react-lite";
import { ConversationServiceManager } from "@multica/conversation-client/services";
import { createFakeTransport } from "@multica/conversation-client/testing";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { NavigationProvider } from "../../../packages/views/navigation";
import { ContextMenu } from "@multica/ui/components/ui/context-menu";
import { LocalConversationSessionRow } from "../../../apps/desktop/src/renderer/src/pages/local-conversation-session-row";
import { LocalConversationListModel } from "../../../apps/desktop/src/renderer/src/platform/local-conversation-list-model";
import { useCompletionRead } from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/components/useCompletionRead";
import "../../../apps/desktop/src/renderer/src/globals.css";

const at = "2026-11-06T12:00:00Z";
const completed = () => sessionStorage.getItem("completed") === "true";
const summary = () => localV2SummarySchema.parse({ conversation_id: "c", title: "Background conversation", summary_seq: completed() ? sessionStorage.getItem("read") === "true" ? 3 : 2 : 1,
  completion: completed() ? { turn_id: "t", turn_index: 0, completion_seq: 1 } : null, read_completion_seq: sessionStorage.getItem("read") === "true" ? 1 : 0, read_turn_index: sessionStorage.getItem("read") === "true" ? 0 : -1,
  has_unread_completion: completed() && sessionStorage.getItem("read") !== "true",
  provider: "codex", status: completed() ? "idle" : "running", active_turn_id: completed() ? undefined : "t", turn_count: 1,
  pinned: false, archived: false, updated_at: at, last_message_preview: completed() ? "The new result is ready" : "Working" });
const transport = createFakeTransport({ userId: "u", workspaceId: "w" }, (_method, path) => {
  if (path.endsWith("/completion-read")) { sessionStorage.setItem("read", "true"); return summary(); }
  return path.includes("/snapshot") ? {
  version: 2, store_generation: "g", snapshot_seq: 5, conversation: summary(),
  turns: [{ id: "t", turn_index: 0, request_id: "r", status: completed() ? "completed" : "running", started_at: at, messages: [], events: [] }],
} : { scope_seq: summary().summary_seq, summaries: [summary()], has_more: false }; });
transport.storeGeneration = async () => "g";
const manager = new ConversationServiceManager({ snapshots: () => ({ read: async () => null, write: async () => {} }) });
const service = manager.get(transport, { userId: "u", workspaceId: "w" });
await service.loadSummaries();
await service.completions!.settled();
const model = new LocalConversationListModel(service.list);
Object.assign(window, { completionFixture: {
  async finish() {
    sessionStorage.setItem("completed", "true");
    service.receive({ version: 2, type: "scope.summaries", scope_seq: 2, summaries: [summary()] });
    await service.completions!.settled();
  },
  flushed: () => service.completions!.settled(),
} });
const navigation = { push() {}, replace() {}, back() {}, pathname: "/test", searchParams: new URLSearchParams(), hash: "", getShareableUrl: (path: string) => path };
const t = ((selector: (value: unknown) => string) => selector({ window: { unread: "Unread" } })) as ComponentProps<typeof LocalConversationSessionRow>["t"];
const App = observer(function App() {
  const [visible, setVisible] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const links = useRef(new Map<string, HTMLAnchorElement>());
  useCompletionRead(service.completions, "c", visible, container);
  return <NavigationProvider value={navigation}>
    <main className="bg-background text-foreground" style={{ minHeight: "100vh", padding: 24 }}>
      <h1>Completion read regression</h1>
      <div style={{ width: 360, marginBlock: 16 }}>
        <ContextMenu><LocalConversationSessionRow model={model} id="c" selected={visible} sessionLinks={links} flowPath="/test" select={() => setVisible(true)}
          updatingId={null} labels={{ pinned: "Pinned", storage: "Saved locally" }} projectLabels={{ actions: "Actions" }} t={t}
          renderSessionActions={() => <></>} setContextSelection={() => {}} chinese={false} remote={false} v2={service} /></ContextMenu>
      </div>
      <button id="open" onClick={() => setVisible(true)}>Open result</button>
      <div data-testid="agent-gui-timeline" style={{ height: 300, width: 600, overflowY: "auto", display: visible ? "block" : "none", border: "1px solid #777" }}>
        <div ref={container}><div style={{ height: 1200 }}>Earlier history</div><div data-multica-turn-id="t">
          <div data-agent-transcript-row="answer" data-agent-transcript-turn-completed="true" style={{ height: 100 }}>The new result is ready</div>
        </div></div>
      </div>
    </main>
  </NavigationProvider>;
});
createRoot(document.getElementById("app")!).render(<App />);
