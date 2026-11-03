export {
  ApiClient,
  ApiError,
  clientErrorMessage,
  dependencyErrorDetails,
  dispatchReasonCode,
  errorCode,
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from "./client";
export type {
  ApiClientOptions,
  ClientRuntimeSnapshot,
  ClientUsageRequest,
  DependencyErrorDetails,
} from "./client";
export type { GlmQuotaStatus, GlmQuotaSnapshot, GlmQuotaWindow } from "./schemas";
export { parseWithFallback, setSchemaLogger } from "./schema";
export { dependencyReadiness } from "./dependency-schemas";
export { IssueGraphSchema, issueGraphReadiness } from "./issue-graph-schemas";
export type { IssueGraph, IssueGraphNode, IssueGraphRequest } from "./issue-graph-schemas";
export {
  BaselineChapterSchema,
  BaselineDecisionSchema,
  BaselineDiffSchema,
  BaselineHistorySchema,
  BaselineRevisionSchema,
  EngineeringConfigSchema,
  EngineeringStageSchema,
  EngineeringOverviewSchema,
  EngineeringUpdatedSchema,
} from "./labrastro-engineering-schemas";
export type {
  BaselineChapter,
  BaselineDecision,
  BaselineDiff,
  BaselineHistory,
  BaselineRevision,
  EngineeringConfig,
  EngineeringStage,
  EngineeringOverview,
} from "./labrastro-engineering-schemas";
export type BaselineChange = import("./labrastro-engineering-schemas").BaselineDiff["added"][number];
export type ChapterState = import("./labrastro-engineering-schemas").EngineeringOverview["chapters"][number];
export type OpenIssue = import("./labrastro-engineering-schemas").BaselineDecision["openIssues"]["inFlight"][number];
export type {
  DependencyView, IssuePrerequisite, IssueWithDependencies,
  CreateIssueWithDependenciesRequest, UpdateIssueWithDependenciesRequest,
  IssueBatchUpdateResult, DependencyMutationFields,
} from "./dependency-schemas";
export type { ParseOptions } from "./schema";
export { DuplicateIssueErrorBodySchema } from "./schemas";
export type { DuplicateIssueErrorBody } from "./schemas";
export { WSClient } from "./ws-client";

import type { ApiClient as ApiClientType } from "./client";

/** Module-level singleton — set once at app boot via `setApiInstance()`. */
let _api: ApiClientType | null = null;

export function setApiInstance(instance: ApiClientType) {
  _api = instance;
}

/** Returns the shared ApiClient singleton. Throws if not yet initialised. */
export function getApi(): ApiClientType {
  if (!_api) throw new Error("ApiClient not initialised — call setApiInstance() first");
  return _api;
}

/**
 * Convenience re-export: a proxy that forwards every property access to the
 * singleton so existing call-sites (`api.listIssues(...)`) keep working.
 */
export const api = new Proxy({} as ApiClientType, {
  get(_target, prop, receiver) {
    // Allow property inspection (HMR/React Refresh) before initialisation
    if (!_api) return undefined;
    const value = Reflect.get(_api, prop, receiver);
    return typeof value === "function" ? value.bind(_api) : value;
  },
});
