import { ApiError } from "../api/client";
import type { KnowledgeState } from "./types";

/**
 * The knowledge state a failed write carries. The server answers 503 with
 * state `offline` when the knowledge service is down, and 409 with the
 * workspace's state (`disabled`, `provisioning`, `failed`) when knowledge is
 * not usable. Returns null for any other failure.
 */
export function knowledgeErrorState(error: unknown): KnowledgeState | null {
  if (!(error instanceof ApiError) || (error.status !== 503 && error.status !== 409)) return null;
  const state = (error.body as { state?: unknown } | undefined)?.state;
  switch (state) {
    case "offline":
    case "disabled":
    case "provisioning":
    case "failed":
      return state;
    default:
      return null;
  }
}
