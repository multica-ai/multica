import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "../types";
import {
  isPiRuntimeModelConfigured,
  runtimeDefaultPiConfig,
  runtimeSupportsDefaultModelConnection,
} from "./model-connection";

function runtime(
  patch: Partial<AgentRuntime> = {},
): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "workspace-1",
    daemon_id: "daemon-1",
    name: "Pi",
    runtime_mode: "local",
    provider: "pi",
    launch_header: "",
    status: "online",
    device_info: "host",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-08-04T00:00:00Z",
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:00Z",
    ...patch,
  };
}

describe("Pi runtime model connection state", () => {
  it("recognizes a complete config plus saved key", () => {
    const value = runtime({
      default_model_config: {
        provider: "deepseek",
        api: "openai-completions",
        base_url: "https://api.deepseek.com",
        model: "deepseek-v4-flash",
      },
      has_default_model_api_key: true,
    });

    expect(runtimeSupportsDefaultModelConnection(value)).toBe(true);
    expect(isPiRuntimeModelConfigured(value)).toBe(true);
    expect(runtimeDefaultPiConfig(value).model).toBe("deepseek-v4-flash");
  });

  it("requires both routing metadata and the key", () => {
    expect(
      isPiRuntimeModelConfigured(
        runtime({
          default_model_config: {
            provider: "deepseek",
            api: "openai-completions",
            base_url: "https://api.deepseek.com",
            model: "deepseek-v4-flash",
          },
          has_default_model_api_key: false,
        }),
      ),
    ).toBe(false);
    expect(
      isPiRuntimeModelConfigured(
        runtime({
          default_model_config: { provider: "deepseek" },
          has_default_model_api_key: true,
        }),
      ),
    ).toBe(false);
  });

  it("keeps older backend responses in legacy mode", () => {
    const value = runtime();
    expect(runtimeSupportsDefaultModelConnection(value)).toBe(false);
    expect(isPiRuntimeModelConfigured(value)).toBe(false);
  });

  it("does not gate non-Pi runtimes", () => {
    expect(isPiRuntimeModelConfigured(runtime({ provider: "codex" }))).toBe(
      true,
    );
  });
});
