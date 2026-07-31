import { describe, expect, it } from "vitest";

import { FAILURE_CLASSES, failureClassOf } from "./failure-class";

describe("failureClassOf", () => {
  it("maps the reasons an operator acts on to distinct classes", () => {
    expect(failureClassOf("agent_error.provider_auth_or_access")).toBe("auth");
    expect(failureClassOf("agent_error.provider_capacity_or_rate_limit")).toBe(
      "rate_limit",
    );
    // Billing quota shares the rate-limit class: same operator response.
    expect(failureClassOf("agent_error.provider_quota_limit")).toBe("rate_limit");
    expect(failureClassOf("timeout")).toBe("timeout");
    expect(failureClassOf("agent_error.agent_timeout")).toBe("timeout");
    expect(failureClassOf("agent_error.provider_server_error")).toBe("provider");
    expect(failureClassOf("runtime_offline")).toBe("runtime");
    expect(failureClassOf("queued_expired")).toBe("runtime");
    // MUL-5370: the run never reached the model provider, so this belongs
    // with the substrate failures an operator fixes by checking the daemon.
    expect(failureClassOf("skill_bundle_unavailable")).toBe("runtime");
    expect(failureClassOf("agent_error.process_failure")).toBe("agent");
  });

  it("keeps pre-MUL-1949 coarse reasons countable", () => {
    // Historical rows still carry the old coarse taxonomy; they must land in
    // a bucket rather than vanish from the class totals.
    expect(failureClassOf("agent_error")).toBe("other");
    expect(failureClassOf("codex_semantic_inactivity")).toBe("timeout");
    expect(failureClassOf("runtime_recovery")).toBe("runtime");
    expect(failureClassOf("manual")).toBe("other");
  });

  it("files a reason from a newer backend under 'other' instead of dropping it", () => {
    // The wire value is an open string on purpose. A client build older than
    // the backend must still count the failure, or the class totals stop
    // reconciling with the headline failure count.
    expect(failureClassOf("agent_error.some_future_reason")).toBe("other");
    expect(failureClassOf("unclassified")).toBe("other");
  });

  it("returns a class every FAILURE_CLASSES member can be produced for", () => {
    // Guards against a class being declared but unreachable — a colour and a
    // translated label with no reason behind it.
    const reachable = new Set(
      [
        "agent_error.provider_auth_or_access",
        "agent_error.provider_quota_limit",
        "timeout",
        "agent_error.provider_network",
        "runtime_offline",
        "agent_error.process_failure",
        "agent_error.unknown",
      ].map(failureClassOf),
    );
    expect([...reachable].toSorted()).toEqual([...FAILURE_CLASSES].toSorted());
  });
});
