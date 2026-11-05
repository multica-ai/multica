import { setTimeout } from "node:timers/promises";
import type { OperationContext } from "../host/jobs.js";
import { problem, VerificationError } from "../host/problem.js";
import { inspectDevice } from "./device-inspect.js";
import { probeTool } from "./inspect.js";
export async function awaitPrerequisites(
  projectRoot: string,
  mode: "renderer" | "desktop",
  required: string[],
  context: OperationContext,
  waiting: (reason: string) => Promise<void>,
) {
  for (;;) {
    context.signal.throwIfAborted();
    const checks = await inspectDevice(
      projectRoot,
      mode,
      probeTool,
      context.signal,
    );
    const missing = checks.filter(
      (check) => required.includes(check.name) && check.status !== "ready",
    );
    if (!missing.length) {
      await context.progress("prerequisitesReady");
      return;
    }
    if (missing.some((check) => check.name === "platform"))
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Full verification preparation requires macOS",
          "userAction",
        ),
      );
    const reason = missing.map((check) => check.name).join(",");
    await waiting(reason);
    await context.awaitingUser(reason);
    await setTimeout(2000, undefined, { signal: context.signal });
  }
}
