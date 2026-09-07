// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import {
  builtInRuntimeIsUsable,
  builtInRuntimeSetupPhase,
  findBuiltInRuntime,
} from "./built-in-runtime-setup";
import { pendingManagedRuntimeFromSetup } from "./managed-runtime-setup";

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "d-1",
    name: "Pi (Mac)",
    runtime_mode: "local",
    provider: "pi",
    launch_header: "pi",
    status: "online",
    device_info: "Mac",
    metadata: {},
    owner_id: null,
    visibility: "private",
    profile_id: null,
    last_seen_at: null,
    created_at: "",
    updated_at: "",
    default_model_config: {},
    has_default_model_api_key: false,
    ...overrides,
  } as AgentRuntime;
}

const configured = {
  default_model_config: {
    provider: "deepseek",
    api: "openai-completions",
    base_url: "https://api.deepseek.com",
    model: "deepseek-v4-flash",
  },
  has_default_model_api_key: true,
};

describe("builtInRuntimeSetupPhase", () => {
  it("offers the install when nothing exists and nothing has started", () => {
    expect(builtInRuntimeSetupPhase({ localDaemonId: "d-1", runtimes: [], setup: null })).toBe("offer");
  });

  it("waits while the binary is downloading", () => {
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [],
        setup: { provider: "pi", phase: "installing", startedAt: "t" },
      }),
    ).toBe("installing");
  });

  it("still waits after the binary lands but before the daemon registers it", () => {
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [],
        setup: { provider: "pi", phase: "ready", startedAt: "t" },
      }),
    ).toBe("installing");
  });

  it("surfaces a failed install", () => {
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [],
        setup: { provider: "pi", phase: "failed", startedAt: "t", error: "no network" },
      }),
    ).toBe("failed");
  });

  it("asks for a key once the runtime registers without a model", () => {
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [runtime()],
        setup: { provider: "pi", phase: "ready", startedAt: "t" },
      }),
    ).toBe("connect");
  });

  it("is ready once the runtime has a complete model connection", () => {
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [runtime(configured)],
        setup: null,
      }),
    ).toBe("ready");
  });

  it("prefers the registered runtime over a stale failed install", () => {
    // A retry that succeeded must not keep showing the earlier failure.
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [runtime(configured)],
        setup: { provider: "pi", phase: "failed", startedAt: "t", error: "boom" },
      }),
    ).toBe("ready");
  });
});

describe("findBuiltInRuntime", () => {
  it("ignores another machine even when its model connection is ready", () => {
    const remote = runtime({ ...configured, daemon_id: "teammate-daemon" });
    const local = runtime({ id: "local-runtime" });
    expect(findBuiltInRuntime([remote, local], "d-1")).toBe(local);
    expect(builtInRuntimeSetupPhase({ runtimes: [remote], localDaemonId: "d-1" })).toBe("offer");
  });

  it("waits for local identity instead of configuring an arbitrary Pi runtime", () => {
    expect(findBuiltInRuntime([runtime(configured)], null)).toBeNull();
  });

  it("does not advertise an offline or cloud runtime as the local installation", () => {
    expect(findBuiltInRuntime([runtime({ status: "offline", ...configured })], "d-1")).toBeNull();
    expect(findBuiltInRuntime([runtime({ runtime_mode: "cloud", ...configured })], "d-1")).toBeNull();
  });

  it("ignores the synthetic row shown while installing", () => {
    const pending = pendingManagedRuntimeFromSetup({
      setup: { provider: "pi", phase: "installing", startedAt: "t" },
      workspaceId: "ws-1",
    });
    // It has no server identity, so nothing can be configured on it.
    expect(findBuiltInRuntime([pending], "d-1")).toBeNull();
    expect(
      builtInRuntimeSetupPhase({ localDaemonId: "d-1",
        runtimes: [pending],
        setup: { provider: "pi", phase: "installing", startedAt: "t" },
      }),
    ).toBe("installing");
  });

  it("ignores a custom-profile runtime that happens to use the same provider", () => {
    expect(findBuiltInRuntime([runtime({ profile_id: "profile-1" })], "d-1")).toBeNull();
  });

  it("ignores other providers", () => {
    expect(findBuiltInRuntime([runtime({ provider: "claude" })], "d-1")).toBeNull();
  });
});

describe("builtInRuntimeIsUsable", () => {
  it("is false for a registered runtime with no model", () => {
    expect(builtInRuntimeIsUsable([runtime()], "d-1")).toBe(false);
  });

  it("is true only once a model connection exists", () => {
    expect(builtInRuntimeIsUsable([runtime(configured)], "d-1")).toBe(true);
  });
});
