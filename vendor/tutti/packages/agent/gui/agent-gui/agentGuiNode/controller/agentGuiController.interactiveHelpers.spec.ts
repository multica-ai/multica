import { describe, expect, it } from "vitest";
import type { AgentActivityInteraction } from "@tutti-os/agent-activity-core";
import type { AgentHostUserProjectsApi } from "../../../host/agentHostApi";
import { normalizeAgentApprovalOptions } from "../../../shared/agentConversation/projection/agentApprovalProjection";
import {
  areAgentGUIUserProjectsEqual,
  interactiveApprovalFromInteraction,
  interactivePromptFromInteraction,
  readAgentGUIUserProjectSnapshot,
  upsertAgentGUIUserProject
} from "./agentGuiController.interactiveHelpers";

describe("interactiveApprovalFromInteraction", () => {
  it("projects the normalized file-edit approval purpose", () => {
    const interaction: AgentActivityInteraction = {
      agentSessionId: "session-1",
      createdAtUnixMs: 1,
      input: {
        callId: "call-1",
        options: [{ optionId: "allow", name: "Allow", kind: "allow_once" }]
      },
      kind: "approval",
      metadata: { approvalPurpose: "edit-files" },
      requestId: "request-1",
      status: "pending",
      toolName: "Approval",
      turnId: "turn-1",
      updatedAtUnixMs: 1
    };

    expect(interactiveApprovalFromInteraction(interaction)).toMatchObject({
      agentSessionId: "session-1",
      approvalPurpose: "edit-files",
      requestId: "request-1",
      turnId: "turn-1"
    });
  });

  it("uses the shared approval-option projection", () => {
    expect(
      normalizeAgentApprovalOptions([
        {
          description: "Apply this once",
          optionId: "allow-once",
          title: "Allow once"
        }
      ])
    ).toEqual([
      {
        description: "Apply this once",
        id: "allow-once",
        kind: "",
        label: "Allow once"
      }
    ]);
  });
});

describe("interactivePromptFromInteraction", () => {
  it("preserves runtime plan options and the keep-planning option id", () => {
    const interaction: AgentActivityInteraction = {
      agentSessionId: "session-1",
      createdAtUnixMs: 1,
      input: {
        options: [
          {
            description: "Auto-approve edits",
            id: "acceptEdits",
            name: "Accept edits"
          },
          { id: "plan", name: "Keep planning" }
        ],
        toolCall: { kind: "switch_mode" }
      },
      kind: "plan",
      requestId: "request-1",
      status: "pending",
      toolName: "ExitPlanMode",
      turnId: "turn-1",
      updatedAtUnixMs: 1
    };

    expect(interactivePromptFromInteraction(interaction)).toEqual({
      agentSessionId: "session-1",
      keepPlanningOptionId: "plan",
      kind: "exit-plan",
      options: [
        {
          description: "Auto-approve edits",
          id: "acceptEdits",
          kind: "acceptEdits",
          label: "Accept edits"
        }
      ],
      requestId: "request-1",
      title: "ExitPlanMode",
      turnId: "turn-1"
    });
  });
});

describe("AgentGUI user-project snapshot projection", () => {
  it("preserves the project order from an already-loaded shared service", () => {
    const api = {
      service: {
        getSnapshot: () => ({
          error: null,
          initialized: true,
          isLoading: false,
          projects: [
            {
              id: "beta",
              label: "Beta",
              path: "/workspace/beta",
              pinnedAtUnixMs: 20,
              sectionKey: "project:/workspace/beta"
            },
            {
              id: "alpha",
              label: "Alpha",
              path: "/workspace/alpha",
              pinnedAtUnixMs: 0,
              sectionKey: "project:/workspace/alpha"
            }
          ],
          revision: 1
        })
      }
    } as AgentHostUserProjectsApi;

    const projects = readAgentGUIUserProjectSnapshot(api);

    expect(projects.map((project) => project.sectionKey)).toEqual([
      "project:/workspace/beta",
      "project:/workspace/alpha"
    ]);
  });

  it("treats a section-key-only change as a new AgentGUI snapshot", () => {
    expect(
      areAgentGUIUserProjectsEqual(
        [
          {
            id: "alpha",
            label: "Alpha",
            path: "/alpha",
            pinnedAtUnixMs: 0
          }
        ],
        [
          {
            id: "alpha",
            label: "Alpha",
            path: "/alpha",
            pinnedAtUnixMs: 0,
            sectionKey: "project:/alpha"
          }
        ]
      )
    ).toBe(false);
  });

  it("treats Windows slash and case differences as the same project", () => {
    expect(
      areAgentGUIUserProjectsEqual(
        [
          {
            id: "project",
            label: "Project",
            path: "C:\\Users\\Demo\\Repo",
            pinnedAtUnixMs: 0
          }
        ],
        [
          {
            id: "project",
            label: "Project",
            path: "c:/users/demo/repo/",
            pinnedAtUnixMs: 0
          }
        ]
      )
    ).toBe(true);
  });

  it("treats a pin-state-only change as a new AgentGUI snapshot", () => {
    expect(
      areAgentGUIUserProjectsEqual(
        [
          {
            id: "alpha",
            label: "Alpha",
            path: "/alpha",
            pinnedAtUnixMs: 0
          }
        ],
        [
          {
            id: "alpha",
            label: "Alpha",
            path: "/alpha",
            pinnedAtUnixMs: 10
          }
        ]
      )
    ).toBe(false);
  });

  it.each(["createdAtUnixMs", "updatedAtUnixMs", "lastUsedAtUnixMs"] as const)(
    "treats a %s-only change as a new AgentGUI snapshot",
    (field) => {
      expect(
        areAgentGUIUserProjectsEqual(
          [
            {
              id: "alpha",
              label: "Alpha",
              path: "/alpha",
              pinnedAtUnixMs: 0,
              [field]: 10
            }
          ],
          [
            {
              id: "alpha",
              label: "Alpha",
              path: "/alpha",
              pinnedAtUnixMs: 0,
              [field]: 20
            }
          ]
        )
      ).toBe(false);
    }
  );

  it("keeps sectionKey when a use result is upserted", () => {
    expect(
      upsertAgentGUIUserProject([], {
        id: "alpha",
        label: "Alpha",
        path: "/alpha",
        pinnedAtUnixMs: 0,
        sectionKey: "project:/alpha"
      })
    ).toEqual([
      {
        id: "alpha",
        label: "Alpha",
        path: "/alpha",
        pinnedAtUnixMs: 0,
        sectionKey: "project:/alpha"
      }
    ]);
  });
});
