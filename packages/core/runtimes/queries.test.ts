import { describe, expect, it } from "vitest";
import {
  runtimeKeys,
  runtimeSetupStatusOptions,
} from "./queries";

describe("runtime setup query options", () => {
  it("keeps secret-minting setup queries outside runtime-list invalidation", () => {
    expect(runtimeKeys.setupCreate("workspace-1")[0]).toBe("runtime-setup");
    expect(runtimeKeys.setupCreate("workspace-1")[0]).not.toBe(
      runtimeKeys.all("workspace-1")[0],
    );
  });

  it("continues polling after a daemon connects with zero runtimes", () => {
    const interval = runtimeSetupStatusOptions(
      "workspace-1",
      "session-1",
    ).refetchInterval;
    expect(typeof interval).toBe("function");
    if (typeof interval !== "function") return;

    const evaluate = interval as (query: {
      state: { data: { runtime_count: number } };
    }) => number | false;
    expect(evaluate({ state: { data: { runtime_count: 0 } } })).toBe(2_000);
    expect(evaluate({ state: { data: { runtime_count: 2 } } })).toBe(false);
  });
});
