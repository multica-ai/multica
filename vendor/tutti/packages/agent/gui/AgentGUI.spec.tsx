import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentGUI, type AgentGUIProps } from "./AgentGUI";

const { agentGuiNodeSpy } = vi.hoisted(() => ({
  agentGuiNodeSpy: vi.fn()
}));

interface AgentGUINodeProbeProps {
  hostCapabilities: {
    agentTargets?: readonly {
      agentTargetId?: string;
      availability: unknown;
      disabled?: boolean;
      ref: unknown;
    }[];
    disabledHomeSuggestions?: readonly string[];
    handoffAgentTargets?: readonly {
      agentTargetId?: string;
      availability: unknown;
      disabled?: boolean;
      ref: unknown;
    }[];
    mentionAgentTargets?: readonly {
      agentTargetId?: string;
      disabled?: boolean;
      iconUrl?: string;
    }[];
    showHandoffTargetOwnershipLabels?: boolean;
  };
}

function createAgentGUIProps(locale: AgentGUIProps["locale"]): AgentGUIProps {
  return {
    agentDirectory: {
      agents: [],
      capturedAtUnixMs: null,
      error: null,
      status: "ready"
    },
    locale,
    frame: {}
  } as unknown as AgentGUIProps;
}

vi.mock("./agent-gui/agentGuiNode/AgentGUINode", async () => {
  const { useTranslation } =
    await vi.importActual<typeof import("./i18n/index")>("./i18n/index");
  const { useOptionalAgentGUIRuntime } = await vi.importActual<
    typeof import("./agentActivityRuntime")
  >("./agentActivityRuntime");
  const { Tooltip, TooltipContent, TooltipTrigger } = await vi.importActual<
    typeof import("@tutti-os/ui-system")
  >("@tutti-os/ui-system");

  return {
    AgentGUINode: (props: AgentGUINodeProbeProps) => {
      agentGuiNodeSpy(props);
      const { t } = useTranslation();
      const activityRuntime = useOptionalAgentGUIRuntime();
      return (
        <>
          <div data-testid="agent-gui-language-probe">
            {t("agentHost.agentGui.newConversation")}
          </div>
          <div data-testid="agent-gui-runtime-probe">
            {activityRuntime ? "provided" : "missing"}
          </div>
          <div data-testid="agent-gui-disabled-suggestions-probe">
            {JSON.stringify(
              props.hostCapabilities.disabledHomeSuggestions ?? []
            )}
          </div>
          <div data-testid="agent-gui-directory-targets-probe">
            {JSON.stringify(
              props.hostCapabilities.agentTargets?.map(
                (target) => target.agentTargetId
              ) ?? []
            )}
          </div>
          <div data-testid="agent-gui-handoff-targets-probe">
            {JSON.stringify(
              props.hostCapabilities.handoffAgentTargets?.map(
                (target) => target.agentTargetId
              ) ?? []
            )}
          </div>
          <div data-testid="agent-gui-mention-targets-probe">
            {JSON.stringify(props.hostCapabilities.mentionAgentTargets ?? [])}
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <button type="button">tooltip probe</button>
            </TooltipTrigger>
            <TooltipContent>AgentGUI tooltip probe</TooltipContent>
          </Tooltip>
        </>
      );
    }
  };
});

describe("AgentGUI i18n", () => {
  beforeEach(() => {
    agentGuiNodeSpy.mockClear();
  });

  it("rerenders agent copy when the host locale changes", () => {
    const { rerender } = render(<AgentGUI {...createAgentGUIProps("en")} />);

    expect(screen.getByTestId("agent-gui-language-probe")).toHaveTextContent(
      "New session"
    );

    rerender(<AgentGUI {...createAgentGUIProps("zh-CN")} />);

    expect(screen.getByTestId("agent-gui-language-probe")).toHaveTextContent(
      "新建会话"
    );
  });

  it("uses the host locale when mounted", () => {
    render(<AgentGUI {...createAgentGUIProps("zh-CN")} />);

    expect(screen.getByTestId("agent-gui-language-probe")).toHaveTextContent(
      "新建会话"
    );
  });

  it("provides the required activity runtime to the AgentGUI node", () => {
    render(
      <AgentGUI
        {...createAgentGUIProps("en")}
        agentActivityRuntime={{} as AgentGUIProps["agentActivityRuntime"]}
      />
    );

    expect(screen.getByTestId("agent-gui-runtime-probe")).toHaveTextContent(
      "provided"
    );
  });

  it("forwards disabled home suggestions to the internal node", () => {
    render(
      <AgentGUI
        {...createAgentGUIProps("en")}
        disabled={["meet-tutti", "import-session"]}
      />
    );

    expect(
      screen.getByTestId("agent-gui-disabled-suggestions-probe")
    ).toHaveTextContent('["meet-tutti","import-session"]');
  });

  it("keeps the runtime directory separate from the handoff launch directory", () => {
    render(
      <AgentGUI
        {...createAgentGUIProps("en")}
        agentDirectory={{
          agents: [agent("local-codex", "codex")],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
        handoffAgentDirectory={{
          agents: [
            agent("local-codex", "codex"),
            agent("shared-agent:claude", "claude-code")
          ],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
      />
    );

    expect(
      screen.getByTestId("agent-gui-directory-targets-probe")
    ).toHaveTextContent('["local-codex"]');
    expect(
      screen.getByTestId("agent-gui-handoff-targets-probe")
    ).toHaveTextContent('["local-codex","shared-agent:claude"]');
  });

  it("disables unavailable setup targets only in the handoff directory", () => {
    render(
      <AgentGUI
        {...createAgentGUIProps("en")}
        agentDirectory={{
          agents: [
            {
              ...agent("extension-kimi", "kimi-code"),
              availability: { status: "unavailable", reason: "probe_failed" },
              setupKind: "target_runtime"
            }
          ],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
      />
    );

    const props = agentGuiNodeSpy.mock.lastCall?.[0] as
      | AgentGUINodeProbeProps
      | undefined;
    expect(props?.hostCapabilities.agentTargets?.[0]?.disabled).toBeUndefined();
    expect(props?.hostCapabilities.handoffAgentTargets?.[0]?.disabled).toBe(
      true
    );
  });

  it("disables handoff targets while their provider readiness check is active", () => {
    const baseProps = createAgentGUIProps("en");
    render(
      <AgentGUI
        {...baseProps}
        agentDirectory={{
          agents: [agent("extension-kimi", "kimi-code")],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
        hostCapabilities={{
          ...baseProps.hostCapabilities,
          providerReadinessGates: {
            "kimi-code": { status: "checking" }
          }
        }}
      />
    );

    const props = agentGuiNodeSpy.mock.lastCall?.[0] as
      | AgentGUINodeProbeProps
      | undefined;
    expect(props?.hostCapabilities.agentTargets?.[0]?.disabled).toBeUndefined();
    expect(props?.hostCapabilities.handoffAgentTargets?.[0]?.disabled).toBe(
      true
    );
  });

  it("keeps unavailable mention identities outside action directories", () => {
    render(
      <AgentGUI
        {...createAgentGUIProps("en")}
        agentDirectory={{
          agents: [agent("local-codex", "codex")],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
        handoffAgentDirectory={{
          agents: [agent("local-codex", "codex")],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
        mentionAgentDirectory={{
          agents: [
            {
              ...agent("shared-agent:offline-codex", "codex"),
              availability: { status: "unavailable", reason: "owner_offline" }
            }
          ],
          capturedAtUnixMs: null,
          error: null,
          status: "ready"
        }}
      />
    );

    expect(
      screen.getByTestId("agent-gui-directory-targets-probe")
    ).toHaveTextContent('["local-codex"]');
    expect(
      screen.getByTestId("agent-gui-handoff-targets-probe")
    ).toHaveTextContent('["local-codex"]');
    expect(
      screen.getByTestId("agent-gui-mention-targets-probe")
    ).toHaveTextContent('"agentTargetId":"shared-agent:offline-codex"');
    expect(
      screen.getByTestId("agent-gui-mention-targets-probe")
    ).toHaveTextContent('"disabled":true');
    expect(
      screen.getByTestId("agent-gui-mention-targets-probe")
    ).toHaveTextContent('"iconUrl":"/shared-agent:offline-codex.png"');
  });

  it("forwards the host-owned handoff ownership presentation", () => {
    render(
      <AgentGUI
        {...createAgentGUIProps("en")}
        hostCapabilities={{ showHandoffTargetOwnershipLabels: true }}
      />
    );

    const props = agentGuiNodeSpy.mock.calls.at(-1)?.[0] as
      | AgentGUINodeProbeProps
      | undefined;
    expect(props?.hostCapabilities.showHandoffTargetOwnershipLabels).toBe(true);
  });

  it("reuses agent target projections across frame-only renders", () => {
    const agents = [agent("local-codex", "codex")];
    const props = {
      ...createAgentGUIProps("en"),
      agentDirectory: {
        agents,
        capturedAtUnixMs: null,
        error: null,
        status: "ready" as const
      }
    };
    const { rerender } = render(<AgentGUI {...props} />);
    const first = agentGuiNodeSpy.mock.calls.at(-1)?.[0] as
      | AgentGUINodeProbeProps
      | undefined;

    rerender(
      <AgentGUI
        {...props}
        frame={{ ...props.frame, width: 960, height: 640 }}
      />
    );
    const second = agentGuiNodeSpy.mock.calls.at(-1)?.[0] as
      | AgentGUINodeProbeProps
      | undefined;

    expect(second?.hostCapabilities.agentTargets).toBe(
      first?.hostCapabilities.agentTargets
    );
    expect(second?.hostCapabilities.handoffAgentTargets).toBe(
      first?.hostCapabilities.handoffAgentTargets
    );
    expect(second?.hostCapabilities.handoffAgentTargets).toBe(
      second?.hostCapabilities.agentTargets
    );
    expect(second?.hostCapabilities.agentTargets?.[0]?.availability).toBe(
      first?.hostCapabilities.agentTargets?.[0]?.availability
    );
    expect(second?.hostCapabilities.agentTargets?.[0]?.ref).toBe(
      first?.hostCapabilities.agentTargets?.[0]?.ref
    );
  });

  it("rebuilds agent target projections when the directory changes", () => {
    const firstAgent = agent("local-codex", "codex");
    const props = {
      ...createAgentGUIProps("en"),
      agentDirectory: {
        agents: [firstAgent],
        capturedAtUnixMs: null,
        error: null,
        status: "ready" as const
      }
    };
    const { rerender } = render(<AgentGUI {...props} />);
    const first = agentGuiNodeSpy.mock.calls.at(-1)?.[0] as
      | AgentGUINodeProbeProps
      | undefined;

    rerender(
      <AgentGUI
        {...props}
        agentDirectory={{
          ...props.agentDirectory,
          agents: [
            {
              ...firstAgent,
              availability: { status: "auth_required" as const }
            }
          ]
        }}
      />
    );
    const second = agentGuiNodeSpy.mock.calls.at(-1)?.[0] as
      | AgentGUINodeProbeProps
      | undefined;

    expect(second?.hostCapabilities.agentTargets).not.toBe(
      first?.hostCapabilities.agentTargets
    );
    expect(second?.hostCapabilities.agentTargets?.[0]?.availability).not.toBe(
      first?.hostCapabilities.agentTargets?.[0]?.availability
    );
    expect(second?.hostCapabilities.agentTargets?.[0]?.ref).not.toBe(
      first?.hostCapabilities.agentTargets?.[0]?.ref
    );
  });
});

function agent(agentTargetId: string, provider: string) {
  return {
    agentTargetId,
    name: agentTargetId,
    iconUrl: `/${agentTargetId}.png`,
    availability: { status: "ready" as const },
    provider
  };
}
