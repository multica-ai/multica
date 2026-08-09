import { describe, expect, it } from "vitest";
import { resolveFailureReasonLabel, resolveWorkflowGateWarningLabel } from "./failure-reason-label";

describe("resolveFailureReasonLabel", () => {
  it("labels every platform-side reason", () => {
    expect(resolveFailureReasonLabel("queued_expired")).toBe("Waited in the queue too long");
    expect(resolveFailureReasonLabel("runtime_offline")).toBe("The computer running the agent went offline");
    expect(resolveFailureReasonLabel("runtime_recovery")).toBe("The agent runner restarted mid-run");
    expect(resolveFailureReasonLabel("timeout")).toBe("The run hit its time limit");
    expect(resolveFailureReasonLabel("iteration_limit")).toBe("The agent hit its step limit");
    expect(resolveFailureReasonLabel("agent_blocked")).toBe("The agent stopped and marked itself blocked");
    expect(resolveFailureReasonLabel("api_invalid_request")).toBe("The model provider rejected the request");
  });

  it("labels every agent-side reason", () => {
    expect(resolveFailureReasonLabel("agent_error.provider_auth_or_access")).toBe("Not signed in to the model provider");
    expect(resolveFailureReasonLabel("agent_error.provider_quota_limit")).toBe("Usage quota is used up");
    expect(resolveFailureReasonLabel("agent_error.provider_capacity_or_rate_limit")).toBe("The model provider was rate-limiting or at capacity");
    expect(resolveFailureReasonLabel("agent_error.provider_server_error")).toBe("The model provider had a server error");
    expect(resolveFailureReasonLabel("agent_error.provider_network")).toBe("The connection to the model provider dropped");
    expect(resolveFailureReasonLabel("agent_error.process_failure")).toBe("The agent process crashed");
    expect(resolveFailureReasonLabel("agent_error.empty_or_unparseable_output")).toBe("The agent returned nothing usable");
    expect(resolveFailureReasonLabel("agent_error.agent_timeout")).toBe("The agent timed out internally");
    expect(resolveFailureReasonLabel("agent_error.context_overflow")).toBe("The conversation grew past the model's limit");
    expect(resolveFailureReasonLabel("agent_error.missing_config")).toBe("The agent is missing required configuration");
    expect(resolveFailureReasonLabel("agent_error.model_not_found_or_unavailable")).toBe("The requested model is unavailable");
    expect(resolveFailureReasonLabel("agent_error.runtime_version_unsupported")).toBe("The agent runner version is too old");
    expect(resolveFailureReasonLabel("agent_error.runtime_missing_executable")).toBe("The agent program is not installed on that computer");
    expect(resolveFailureReasonLabel("agent_error.unknown")).toBe("The agent failed for an unrecognised reason");
  });

  it("degrades gracefully on an unknown future agent_error sub-reason", () => {
    expect(resolveFailureReasonLabel("agent_error.brand_new_thing")).toBe("Agent error: brand new thing");
  });

  it("degrades gracefully on a wholly unknown reason", () => {
    expect(resolveFailureReasonLabel("something_else_entirely")).toBe("Something else entirely");
  });

  it("returns null when there is no reason", () => {
    expect(resolveFailureReasonLabel("")).toBeNull();
    expect(resolveFailureReasonLabel(null)).toBeNull();
    expect(resolveFailureReasonLabel(undefined)).toBeNull();
  });
});

describe("resolveWorkflowGateWarningLabel", () => {
  it("names the Hook that warned without turning the run into a failure", () => {
    expect(resolveWorkflowGateWarningLabel({
      output: "done",
      completion_warning: {
        code: "workflow_gate_rejected",
        hook_id: "hook-1",
        hook_name: "Require evidence before an agent run stops",
        requirement: "Create a wakeup",
        attempt: 2,
      },
    })).toBe("Stopped by hook: Require evidence before an agent run stops");
  });

  it("ignores malformed or unrelated task results", () => {
    expect(resolveWorkflowGateWarningLabel(null)).toBeNull();
    expect(resolveWorkflowGateWarningLabel({ completion_warning: { code: "other" } })).toBeNull();
  });
});
