// @vitest-environment jsdom
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import type { ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "../api";
import { WorkspaceSlugProvider } from "../paths";
import type { Issue, UpdateIssueRequest, Workspace } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { useIssueDescriptionSave } from "./description-save";
import { issueKeys } from "./queries";

const workspace: Workspace = {
  id: "22222222-2222-4222-8222-222222222222",
  slug: "description-save-test",
  name: "Description save test",
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "SAVE",
  avatar_url: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const issue: Issue = {
  id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
  workspace_id: workspace.id,
  number: 1,
  identifier: "SAVE-1",
  title: "Description save",
  description: "Original",
  status: "todo",
  priority: "none",
  creator_type: "member",
  creator_id: "11111111-1111-4111-8111-111111111111",
  assignee_type: null,
  assignee_id: null,
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  revision: 1,
};

describe("useIssueDescriptionSave", () => {
  it.each([
    { attachmentIds: ["kept-attachment"] },
    { attachmentIds: [] },
  ])("sends the queued draft after unmount with attachments $attachmentIds", async ({ attachmentIds }) => {
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: Infinity },
        mutations: { retry: false },
      },
    });
    client.setQueryData(workspaceKeys.list(), [workspace]);
    client.setQueryData(issueKeys.detail(workspace.id, issue.id), issue);
    const onError = vi.fn();
    const requests: { method: string | undefined; url: string | undefined; body: UpdateIssueRequest }[] = [];
    const transportErrors: unknown[] = [];
    let releaseFirst!: () => void;
    const firstResponse = new Promise<void>((resolve) => { releaseFirst = resolve; });
    const server = createServer(async (request, response) => {
      try {
        let body = "";
        for await (const chunk of request) body += chunk;
        const payload: UpdateIssueRequest = JSON.parse(body);
        requests.push({ method: request.method, url: request.url, body: payload });
        const revision = requests.length + 1;
        if (requests.length === 1) await firstResponse;
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ ...issue, description: payload.description, revision }));
      } catch (error) {
        transportErrors.push(error);
        response.writeHead(500).end();
      }
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const { port } = server.address() as AddressInfo;
    setApiInstance(new ApiClient(`http://127.0.0.1:${port}`));
    function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={client}>
          <WorkspaceSlugProvider slug={workspace.slug}>{children}</WorkspaceSlugProvider>
        </QueryClientProvider>
      );
    }
    try {
      const { result, unmount } = renderHook(() => useIssueDescriptionSave(issue.id, onError), {
        wrapper: Wrapper,
      });
      act(() => result.current({
        markdown: "D1", baseMarkdown: "Original", attachmentIds: ["first-attachment"],
      }));
      await waitFor(() => expect(requests).toHaveLength(1));
      act(() => result.current({ markdown: "D2", baseMarkdown: "Original", attachmentIds }));
      expect(requests).toHaveLength(1);
      expect(client.getQueryData<Issue>(issueKeys.detail(workspace.id, issue.id))?.description)
        .toBe("Original");

      unmount();
      await act(async () => { releaseFirst(); });
      await waitFor(() => expect(requests).toHaveLength(2));
      await waitFor(() => expect(client.isMutating()).toBe(0));

      expect(requests).toEqual([
        {
          method: "PUT", url: `/api/issues/${issue.id}`,
          body: { description: "D1", description_base: "Original", attachment_ids: ["first-attachment"] },
        },
        {
          method: "PUT", url: `/api/issues/${issue.id}`,
          body: {
            description: "D2", description_base: "D1",
            ...(attachmentIds.length > 0 ? { attachment_ids: attachmentIds } : {}),
          },
        },
      ]);
      expect(client.getQueryData<Issue>(issueKeys.detail(workspace.id, issue.id))?.description)
        .toBe("D2");
      expect(onError).not.toHaveBeenCalled();
      expect(transportErrors).toEqual([]);
    } finally {
      releaseFirst();
      cleanup();
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
      client.clear();
    }
  });
});
