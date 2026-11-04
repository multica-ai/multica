// Test rig: runs the GUI against a host page's MessagePort so e2e specs can feed it fake data.
import { createRoot } from "react-dom/client";
import AgentGuiMount from "../src/mount";
import { localSourceFromClient } from "../src/runtime/local-snapshot-test-host";
import { connectAgentGuiHost } from "./rpc-client";
import { agentGuiMessageTargetOrigin } from "./frame-messaging";

const connection = await connectAgentGuiHost();
document.documentElement.dataset.theme = connection.snapshot.theme;
document.documentElement.lang = connection.snapshot.locale;
window.addEventListener("pagehide", () => connection.dispose(), { once: true });
// Specs describe conversations as session.load details; this builds them from those.
const localConversations = localSourceFromClient(connection.client as never);
createRoot(document.getElementById("app")!).render(
  <AgentGuiMount client={connection.client} initial={connection.snapshot} localConversations={localConversations}
    onRendered={() => window.parent.postMessage({ type: "multica:agent-gui:rendered", version: 1 }, agentGuiMessageTargetOrigin())} />,
);
