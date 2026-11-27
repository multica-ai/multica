// @vitest-environment node
import { afterEach, expect, it, vi } from 'vitest';
import { ApiClient } from './client';
import { setCurrentWorkspace } from '../platform/workspace-storage';
const issueId='00000000-0000-4000-8000-000000000010';
const workspaceId='00000000-0000-4000-8000-000000000001';
const graph={root_issue_id:issueId,nodes:[],edges:[],findings:[],evidence:[],coverage:{complete:true,truncated:false,warnings:[]}};
afterEach(() => {
  vi.unstubAllGlobals();
  setCurrentWorkspace(null, null);
});
it('reads graph paths with explicit workspace and cancellation, without capability requests',async()=>{
 const fetcher=vi.fn().mockImplementation(async()=>new Response(JSON.stringify(graph),{status:200}));vi.stubGlobal('fetch',fetcher);
 const client=new ApiClient('https://synthetic.example');const controller=new AbortController();
 await client.listTaskTrajectory([issueId],{workspaceId,signal:controller.signal});
 expect(fetcher.mock.calls[0]?.[0]).toBe('https://synthetic.example/api/issues/trajectory');
 expect(fetcher.mock.calls[0]?.[1]).toMatchObject({method:'POST',signal:controller.signal,headers:{'X-Workspace-ID':workspaceId,'X-Workspace-Slug':''},body:JSON.stringify({issue_ids:[issueId]})});
 await client.getIssueTrajectory(issueId,{workspaceId,signal:controller.signal});
 expect(fetcher.mock.calls[1]?.[0]).toBe(`https://synthetic.example/api/issues/${issueId}/trajectory?scope=issue`);
});
it('keeps malformed graph visible as incomplete without logging received body',async()=>{
 const secret='F-SYN-1 malformed sensitive body';vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({nodes:secret}),{status:200})));
 const client=new ApiClient('https://synthetic.example');
 const result=await client.getIssueTrajectory(issueId,{workspaceId});
 expect(result.nodes).toEqual([]);expect(result.coverage.complete).toBe(false);expect(result.coverage.warnings.length).toBeGreaterThan(0);expect(JSON.stringify(result)).not.toContain(secret);
});
it('passes cancellation and workspace through native membership and messages reads',async()=>{
 const fetcher=vi.fn().mockResolvedValue(new Response('[]',{status:200}));vi.stubGlobal('fetch',fetcher);
 const client=new ApiClient('https://synthetic.example');const signal=new AbortController().signal;
 await client.listTrajectoryTaskMessages(issueId,{workspaceId,signal});
 expect(fetcher.mock.calls[0]?.[1]).toMatchObject({signal,headers:{'X-Workspace-ID':workspaceId}});
});
it('keeps a graph error body out of request diagnostics and thrown error messages',async()=>{
 const secret='F-SYN-1 raw graph diagnostic body';const logger={debug:vi.fn(),info:vi.fn(),warn:vi.fn(),error:vi.fn()};
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async()=>new Response(JSON.stringify({error:secret}),{status:503})));
 const client=new ApiClient('https://synthetic.example',{logger});
 await expect(client.getIssueTrajectory(issueId,{workspaceId})).rejects.not.toThrow(secret);
 expect(JSON.stringify(logger.error.mock.calls)).not.toContain(secret);
});
it('rejects malformed native messages without logging the received graph-log body',async()=>{
 const secret='F-SYN-1 malformed log diagnostic';const {setSchemaLogger}=await import('./schema');const logger={debug:vi.fn(),info:vi.fn(),warn:vi.fn(),error:vi.fn()};setSchemaLogger(logger);
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async()=>new Response(JSON.stringify({secret}),{status:200})));
 const client=new ApiClient('https://synthetic.example',{logger});
 try {await expect(client.listTrajectoryTaskMessages(issueId,{workspaceId})).rejects.toThrow('trajectory_invalid_response');expect(JSON.stringify(logger.warn.mock.calls)).not.toContain(secret);} finally {setSchemaLogger({debug:()=>{},info:()=>{},warn:()=>{},error:()=>{}});}
});
it.each([true, false])("keeps a Graph 401 on Trajectory, aborted=%s", async aborted => {
  let reply!: (response: Response) => void;
  vi.stubGlobal("fetch", vi.fn().mockImplementation(() => new Promise(resolve => { reply = resolve; })));
  const onUnauthorized = vi.fn();
  const controller = new AbortController();
  const client = new ApiClient("https://synthetic.example", { onUnauthorized });
  const pending = expect(client.getIssueTrajectory(issueId, { workspaceId, signal: controller.signal }))
    .rejects.toMatchObject({ status: 401 });
  await vi.waitFor(() => expect(reply).toBeTypeOf("function"));
  if (aborted) controller.abort();
  reply(new Response("{}", { status: 401 }));
  await pending;
  expect(onUnauthorized).not.toHaveBeenCalled();
});

it("uses the page workspace for filter reads and isolates their 401 from native requests", async () => {
  const fetcher = vi.fn().mockImplementation(async () => new Response("{}", { status: 401 }));
  vi.stubGlobal("fetch", fetcher);
  setCurrentWorkspace("other-workspace", issueId);
  const onUnauthorized = vi.fn();
  const client = new ApiClient("https://synthetic.example", { onUnauthorized });
  const signal = new AbortController().signal;
  await expect(client.readTrajectoryFilterMetadata("/api/issues/unread", { workspaceId, signal }))
    .rejects.toMatchObject({ status: 401 });
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({
    signal, headers: { "X-Workspace-ID": workspaceId, "X-Workspace-Slug": "" },
  });
  expect(onUnauthorized).not.toHaveBeenCalled();

  await expect(client.request("/api/issues/unread")).rejects.toMatchObject({ status: 401 });
  expect(fetcher.mock.calls[1]?.[1]).toMatchObject({ headers: { "X-Workspace-Slug": "other-workspace" } });
  expect(onUnauthorized).toHaveBeenCalledOnce();
});

it("does not change the native CSRF preference after a Trajectory retry", async () => {
  vi.stubGlobal("document", { cookie: "multica_csrf_session=session; multica_csrf=token" });
  const fetcher = vi.fn()
    .mockResolvedValueOnce(new Response('{"error":"CSRF validation failed"}', { status: 403 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(graph)))
    .mockResolvedValueOnce(new Response("{}"));
  vi.stubGlobal("fetch", fetcher);
  const client = new ApiClient("https://synthetic.example");
  await client.listTaskTrajectory([issueId], { workspaceId });
  await client.request("/api/issues/unread");
  expect(fetcher.mock.calls.map(([, request]) => request.headers["X-CSRF-Token"]))
    .toEqual(["session", "token", "session"]);
});

it('reads the workspace capability and shows Trajectory only on an explicit enabled: true', async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ enabled: true }), { status: 200 }));
  vi.stubGlobal('fetch', fetcher);
  const client = new ApiClient('https://synthetic.example');
  await expect(client.getTrajectoryCapability(workspaceId)).resolves.toEqual({ enabled: true });
  expect(fetcher.mock.calls[0]?.[0]).toBe('https://synthetic.example/api/trajectory/capability');
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({ headers: { 'X-Workspace-ID': workspaceId } });
});

it.each([{}, { enabled: 'true' }, { enabled: 1 }, null, []])('treats malformed capability %j as disabled', async body => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })));
  const client = new ApiClient('https://synthetic.example');
  await expect(client.getTrajectoryCapability(workspaceId)).resolves.toEqual({ enabled: false });
});
