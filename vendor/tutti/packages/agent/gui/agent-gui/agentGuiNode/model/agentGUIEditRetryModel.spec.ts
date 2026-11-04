import { describe, expect, it } from "vitest";
import type { AgentGUIEditRetryAvailability } from "../controller/agentGuiController.promptOperations";
import { projectAgentGUIEditRetryPresentation } from "./agentGUIEditRetryModel";

describe("projectAgentGUIEditRetryPresentation", () => {
  it("treats prepared eligible history as ready", () => {
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({
          eligible: true,
          recoveryState: "prepared",
          turnId: "turn-latest"
        }),
        commandStatus: "idle"
      })
    ).toMatchObject({
      editableTurnId: "turn-latest",
      state: "ready"
    });
  });

  it("maps command submission, authoritative reconciliation, and rollback to processing", () => {
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({ recoveryState: "rolling_back" }),
        commandStatus: "idle"
      }).state
    ).toBe("processing");
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({ recoveryState: "prepared" }),
        commandStatus: "pending"
      }).state
    ).toBe("processing");
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({
          eligible: true,
          recoveryState: "prepared",
          turnId: "turn-latest"
        }),
        commandStatus: "reconciling"
      })
    ).toMatchObject({
      editableTurnId: null,
      state: "processing"
    });
  });

  it("maps resend_pending to explicit recovery after rollback is confirmed", () => {
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({
          availableActions: ["reconcile", "retry_replacement"],
          operationId: "operation-1",
          recoveryState: "resend_pending"
        }),
        commandStatus: "idle"
      })
    ).toMatchObject({
      availableActions: ["reconcile", "retry_replacement"],
      editableTurnId: null,
      operationId: "operation-1",
      state: "needs_action"
    });
  });

  it("shows explicit recovery without blocking an eligible retry after a transient command failure", () => {
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({
          eligible: true,
          recoveryState: "prepared",
          turnId: "turn-latest"
        }),
        commandStatus: "failed"
      })
    ).toMatchObject({
      editableTurnId: "turn-latest",
      state: "ready"
    });
    expect(
      projectAgentGUIEditRetryPresentation({
        availability: availability({
          availableActions: ["reconcile"],
          recoveryState: "recovery_required"
        }),
        commandStatus: "idle"
      })
    ).toMatchObject({
      availableActions: ["reconcile"],
      editableTurnId: null,
      state: "needs_action"
    });
  });
});

function availability(
  overrides: Partial<AgentGUIEditRetryAvailability> = {}
): AgentGUIEditRetryAvailability {
  return {
    supported: true,
    eligible: false,
    historyRevision: 1,
    recoveryState: "prepared",
    availableActions: [],
    ...overrides
  };
}
