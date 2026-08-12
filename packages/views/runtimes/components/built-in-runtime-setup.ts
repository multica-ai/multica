import { isPiRuntimeModelConfigured } from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { isPendingManagedRuntime, type ManagedRuntimeSetupStatus } from "./managed-runtime-setup";

/** The provider Multica installs and manages on the user's behalf. */
export const BUILT_IN_RUNTIME_PROVIDER = "pi";

/**
 * Where the user is in "get a working runtime without a terminal":
 *
 *   offer      nothing installed, nothing started — show the pitch
 *   installing binary is downloading, or installed and awaiting registration
 *   failed     install failed; the reason travels with it
 *   connect    runtime is registered but has no model — ask for the API key
 *   ready      registered and configured; agents can run
 */
export type BuiltInRuntimeSetupPhase =
  | "offer"
  | "installing"
  | "failed"
  | "connect"
  | "ready";

/**
 * The registered built-in runtime, if the daemon has reported one.
 *
 * Excludes the synthetic row the Runtimes list renders during install (it has
 * no server identity, so nothing can be configured on it) and custom-profile
 * runtimes that merely happen to use the same provider.
 */
export function findBuiltInRuntime(
  runtimes: readonly AgentRuntime[],
): AgentRuntime | null {
  return (
    runtimes.find(
      (runtime) =>
        runtime.provider === BUILT_IN_RUNTIME_PROVIDER &&
        !runtime.profile_id &&
        !isPendingManagedRuntime(runtime),
    ) ?? null
  );
}

export function builtInRuntimeSetupPhase({
  runtimes,
  setup,
}: {
  runtimes: readonly AgentRuntime[];
  setup?: ManagedRuntimeSetupStatus | null;
}): BuiltInRuntimeSetupPhase {
  const runtime = findBuiltInRuntime(runtimes);
  if (runtime) {
    // Once the runtime exists, the install outcome stops mattering: what the
    // user still needs is a model, or nothing at all.
    return isPiRuntimeModelConfigured(runtime) ? "ready" : "connect";
  }
  if (setup?.phase === "failed") return "failed";
  // "ready" here means the binary landed but the daemon has not registered it
  // yet. That is still waiting from the user's point of view.
  if (setup?.phase === "installing" || setup?.phase === "ready") {
    return "installing";
  }
  return "offer";
}

/**
 * True once the user has a runtime that can actually execute work. Onboarding
 * uses this to decide whether continuing means "start working" or "you still
 * have a step left".
 */
export function builtInRuntimeIsUsable(
  runtimes: readonly AgentRuntime[],
): boolean {
  const runtime = findBuiltInRuntime(runtimes);
  return runtime !== null && isPiRuntimeModelConfigured(runtime);
}
