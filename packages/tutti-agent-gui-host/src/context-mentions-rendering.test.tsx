// @vitest-environment jsdom
import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";
import { RichTextMentionServiceProvider } from "@tutti-os/ui-rich-text/editor";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect,it,vi } from "vitest";
import { AgentMessageMarkdown } from "../../../vendor/tutti/packages/agent/gui/shared/AgentMessageMarkdown";
import { AgentRichTextReadonly } from "../../../vendor/tutti/packages/agent/gui/shared/AgentRichTextReadonly";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { createMulticaContextMentions } from "./context-mentions";
import type { AgentGuiHostServices,AgentGuiWorkspaceState } from "./protocol";

it.each([
  ["agent-target", "markdown"],
  ["workspace-member", "markdown"],
  ["workspace-member", "readonly"]
])("renders the %s avatar and name in %s messages", async (providerId, renderer) => {
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
    members: [{ userId: "bug-hunter", name: "Bug hunter", iconUrl }],
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
    const provider = service.getProvider(providerId)!;
    const items = await provider.query({
      keyword: "",
      trigger: "@",
      context: {}
    });
    const inserted = provider.toInsertResult(items[0]);
    if (inserted.kind !== "mention") throw new Error("Expected a reference");
    const content = `${createRichTextMentionMarkdown({ providerId, ...inserted.mention })} Please record this bug`;
    await act(async () =>
      root.render(
        <RichTextMentionServiceProvider service={service}>
          {renderer === "readonly" ? (
            <AgentRichTextReadonly value={content} />
          ) : (
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
          )}
        </RichTextMentionServiceProvider>
      )
    );
    const mention = container.querySelector(
      `[data-agent-mention-kind="${providerId}"]`
    );
    expect(mention?.textContent).toBe("Bug hunter");
    expect(mention?.textContent).not.toContain("bug-hunter");
    expect(mention?.querySelector("img")?.getAttribute("src")).toBe(iconUrl);
    expect(mention?.querySelector<HTMLElement>('[data-slot="mention-pill"]')?.style.color).toBe("var(--rich-text-mention-session)");
  } finally {
    await act(async () => root.unmount());
    container.remove();
    service.dispose();
    vi.unstubAllGlobals();
  }
});
