import { z } from "zod";
import type { Problem } from "../contracts/index.js";

export class VerificationError extends Error {
  constructor(readonly problem: Problem) {
    super(problem.message);
  }
}

export function problem(
  code: Problem["code"],
  message: string,
  recovery: Problem["recovery"] = "none",
  relatedRefs: string[] = [],
): Problem {
  return { code, message, recovery, relatedRefs };
}

export function toProblem(error: unknown): Problem {
  if (error instanceof VerificationError) return error.problem;
  if (error instanceof z.ZodError)
    return problem(
      "invalidInput",
      "The input or stored record does not match its schema",
      "inspect",
    );
  if (error instanceof Error && error.name === "AbortError")
    return problem("cancelled", "The operation was cancelled", "inspect");
  return problem(
    "internalError",
    "The verification operation failed; inspect the task and retained resources",
    "inspect",
  );
}

export function isMissing(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "ENOENT";
}
