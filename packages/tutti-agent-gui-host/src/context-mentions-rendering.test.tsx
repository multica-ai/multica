// @vitest-environment jsdom
import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";
import { RichTextMentionServiceProvider } from "@tutti-os/ui-rich-text/editor";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect,it,vi } from "vitest";
import { AgentMessageMarkdown } from "../../../vendor/tutti/packages/agent/gui/shared/AgentMessageMarkdown";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { createMulticaContextMentions } from "./context-mentions";
import type { AgentGuiHostServices,AgentGuiWorkspaceState } from "./protocol";

it("renders the workspace agent avatar in a sent message whose target directory contains only local Codex", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const iconUrl = "https://example.test/bug-hunter.png";
  const state = {
    agents: [
      {
        agent: { id: "bug-hunter", name: "Bug hunter" },
        provider: "codex",
        iconUrl,
        canInvoke: true,
        runtimeBound: true
      }
    ],
    sessions: []
  } as unknown as AgentGuiWorkspaceState;
  const service = createMulticaContextMentions(
    fakeHostServices(vi.fn()) as unknown as AgentGuiHostServices,
    { workspaceId: "ws", local: true },
    {
      getWorkspaceState: () => state,
      subscribeWorkspace: () => () => {}
    }
  );
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  try {
    const provider = service.getProvider("agent-target")!;
    const items = await provider.query({
      keyword: "",
      trigger: "@",
      context: {}
    });
    const inserted = provider.toInsertResult(items[0]);
    if (inserted.kind !== "mention") throw new Error("Expected a reference");
    const content = `${createRichTextMentionMarkdown({ providerId: "agent-target", ...inserted.mention })} Please record this bug`;
    await act(async () =>
      root.render(
        <RichTextMentionServiceProvider service={service}>
          <AgentMessageMarkdown
            content={content}
            agentTargets={[
              {
                agentTargetId: "local:codex",
                provider: "codex",
                name: "Codex",
                iconUrl: "https://example.test/codex.png"
              }
            ]}
          />
        </RichTextMentionServiceProvider>
      )
    );
    const mention = container.querySelector(
      '[data-agent-mention-kind="agent-target"]'
    );
    expect(mention?.textContent).toContain("Bug hunter");
    expect(mention?.querySelector("img")?.getAttribute("src")).toBe(iconUrl);
  } finally {
    await act(async () => root.unmount());
    container.remove();
    service.dispose();
    vi.unstubAllGlobals();
  }
});
