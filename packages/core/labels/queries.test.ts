import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { IssueLabelsResponse, Label, ListLabelsResponse } from "../types";
import {
  issueLabelsOptions,
  labelKeys,
  labelListOptions,
  resourceLabelsOptions,
} from "./queries";

const WS_ID = "ws-1";
const ISSUE_ID = "issue-1";

const label: Label = {
  id: "label-1",
  workspace_id: WS_ID,
  name: "bug",
  color: "#ef4444",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("labelKeys", () => {
  it("keeps list and resource caches scoped to their workspace and resource type", () => {
    expect(labelKeys.list(WS_ID)).toEqual(["labels", WS_ID, "list", "issue"]);
    expect(labelKeys.list(WS_ID, "skill")).toEqual([
      "labels",
      WS_ID,
      "list",
      "skill",
    ]);
    expect(labelKeys.byIssue(WS_ID, ISSUE_ID)).toEqual([
      "labels",
      WS_ID,
      "issue",
      ISSUE_ID,
    ]);
    expect(labelKeys.byResource(WS_ID, "agent", "agent-1")).toEqual([
      "labels",
      WS_ID,
      "agent",
      "agent-1",
    ]);
    expect(labelKeys.list("ws-2")).not.toEqual(labelKeys.list(WS_ID));
  });
});

describe("label query options", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("fetches the label list and selects its labels", async () => {
    const response: ListLabelsResponse = { labels: [label], total: 1 };
    const listLabels = vi.fn().mockResolvedValue(response);
    setApiInstance({ listLabels } as unknown as ApiClient);
    const options = labelListOptions(WS_ID);

    await expect(qc.fetchQuery(options)).resolves.toEqual(response);
    expect(listLabels).toHaveBeenCalledWith("issue");
    expect(options.select?.(response)).toEqual([label]);
  });

  it("forwards a non-default resource type to the label list request", async () => {
    const response: ListLabelsResponse = { labels: [label], total: 1 };
    const listLabels = vi.fn().mockResolvedValue(response);
    setApiInstance({ listLabels } as unknown as ApiClient);

    await qc.fetchQuery(labelListOptions(WS_ID, "skill"));

    expect(listLabels).toHaveBeenCalledWith("skill");
  });

  it("propagates label list request failures without caching data", async () => {
    const error = new Error("failed to list labels");
    const listLabels = vi.fn().mockRejectedValue(error);
    setApiInstance({ listLabels } as unknown as ApiClient);
    const options = labelListOptions(WS_ID);

    await expect(qc.fetchQuery(options)).rejects.toBe(error);
    expect(qc.getQueryData(options.queryKey)).toBeUndefined();
  });

  it("fetches labels for the requested issue and selects its labels", async () => {
    const response: IssueLabelsResponse = { labels: [label] };
    const listLabelsForIssue = vi.fn().mockResolvedValue(response);
    setApiInstance({ listLabelsForIssue } as unknown as ApiClient);
    const options = issueLabelsOptions(WS_ID, ISSUE_ID);

    await expect(qc.fetchQuery(options)).resolves.toEqual(response);
    expect(listLabelsForIssue).toHaveBeenCalledWith(ISSUE_ID);
    expect(options.select?.(response)).toEqual([label]);
  });

  it("disables issue label requests until an issue id exists", () => {
    expect(issueLabelsOptions(WS_ID, "").enabled).toBe(false);
    expect(issueLabelsOptions(WS_ID, ISSUE_ID).enabled).toBe(true);
  });

  it("disables generic resource requests until a resource id exists", () => {
    expect(resourceLabelsOptions(WS_ID, "agent", "").enabled).toBe(false);
    expect(resourceLabelsOptions(WS_ID, "agent", "agent-1").enabled).toBe(true);
  });

  it("propagates issue label request failures without caching data", async () => {
    const error = new Error("failed to list issue labels");
    const listLabelsForIssue = vi.fn().mockRejectedValue(error);
    setApiInstance({ listLabelsForIssue } as unknown as ApiClient);
    const options = issueLabelsOptions(WS_ID, ISSUE_ID);

    await expect(qc.fetchQuery(options)).rejects.toBe(error);
    expect(qc.getQueryData(options.queryKey)).toBeUndefined();
  });
});
