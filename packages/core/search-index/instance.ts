import { api } from "../api";
import type { SearchIssuesResponse, SearchProjectsResponse } from "../types";
import { LocalSearchIndexClient, startSearchIndexWorker } from "./client";

let client: LocalSearchIndexClient | null = null;

/** The tab-wide local search index (MUL-7754). */
export function getLocalSearchIndex(): LocalSearchIndexClient {
  client ??= new LocalSearchIndexClient(() => api, startSearchIndexWorker);
  return client;
}

type SearchParams = Parameters<typeof api.searchIssues>[0];

/**
 * Issue search that answers from the local index when it can and asks the
 * server otherwise. Results have the server's shape and order either way.
 */
export async function searchIssues(params: SearchParams): Promise<SearchIssuesResponse> {
  const local = await getLocalSearchIndex().searchIssues(params);
  return local ?? api.searchIssues(params);
}

/** Project search with the same local-first behavior as `searchIssues`. */
export async function searchProjects(params: SearchParams): Promise<SearchProjectsResponse> {
  const local = await getLocalSearchIndex().searchProjects(params);
  return local ?? api.searchProjects(params);
}

/** Whether searches in this tab are currently answered locally. */
export function isLocalSearchReady(): boolean {
  return getLocalSearchIndex().isServing();
}

/** Destroys this device's local copies of a workspace the user can no longer read. */
export function forgetLocalSearchIndex(workspaceId: string): Promise<void> {
  return getLocalSearchIndex().forget(workspaceId);
}

/** Deletes every local search index on this device. */
export function wipeLocalSearchIndex(): Promise<void> {
  return getLocalSearchIndex().wipe();
}
