import { describe, expect, it } from "vitest";

import { blockedReasonLabel, blockedShortReasonLabel } from "./blocked-trigger-copy";
import en from "../locales/en/issues.json";

// A blocked mention's copy has to name the fix the user actually has. An unbound
// agent (agent_runtime_required) and an agent whose machine is offline
// (runtime_offline) need opposite actions — bind a runtime vs. bring the machine
// back — so they must never collapse onto the same label. Before MUL-5559 the
// server reported unbound agents as runtime_offline, which told people to
// reconnect a computer that no longer existed.
type Leaf = string | Record<string, unknown>;

// Minimal stand-in for the i18n `t` used by the copy helpers: resolves the
// selector against the real en bundle so the test fails if a key is missing.
const t = ((selector: (bundle: unknown) => Leaf) => {
  const value = selector(en as unknown);
  if (typeof value !== "string") {
    throw new Error("selector did not resolve to a string");
  }
  return value;
}) as Parameters<typeof blockedReasonLabel>[1];

describe("blocked trigger copy", () => {
  it("distinguishes an unbound agent from an offline runtime", () => {
    const unbound = blockedReasonLabel("agent_runtime_required", t);
    const offline = blockedReasonLabel("runtime_offline", t);

    expect(unbound).not.toBe(offline);
    expect(unbound).toBe(en.comment.trigger_blocked_agent_runtime_required);
    expect(offline).toBe(en.comment.trigger_blocked_runtime_offline);
    // The unbound label must not tell the user their runtime is offline.
    expect(unbound.toLowerCase()).not.toContain("offline");
  });

  it("distinguishes them in the short chip label too", () => {
    const unbound = blockedShortReasonLabel("agent_runtime_required", t);
    const offline = blockedShortReasonLabel("runtime_offline", t);

    expect(unbound).not.toBe(offline);
    expect(unbound).toBe(en.comment.trigger_blocked_short_agent_runtime_required);
  });

  it("degrades an unknown code to the generic label", () => {
    expect(blockedReasonLabel("some_future_code", t)).toBe(en.comment.trigger_blocked_generic);
    expect(blockedShortReasonLabel("some_future_code", t)).toBe(
      en.comment.trigger_blocked_short_generic,
    );
  });
});
