// @vitest-environment node
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { ApiClient, setApiInstance } from '../api';
const apiClient=new ApiClient('https://synthetic.example');
beforeEach(()=>setApiInstance(apiClient));
import { GRAPH_EVENT_COALESCE_MS, attachGraphLifecycle, closeGraphScope, graphIssueOptions, graphKeys, membershipOptions, isGraphScopeClosed, openGraphScope } from './graph-web';
import type { WSEventType } from '../types/events';
const scope={accountId:'00000000-0000-4000-8000-000000000001',workspaceId:'00000000-0000-4000-8000-000000000002'};
const issueId='00000000-0000-4000-8000-000000000010', runId='00000000-0000-4000-8000-000000000011',commentId='00000000-0000-4000-8000-000000000012';
const secret='F-SYN-1 deleted trigger body';
const graph={root_issue_id:issueId,nodes:[{id:`issue:${issueId}`,kind:'issue',issue_id:issueId,title:'Synthetic',status:'todo',created_at:'',details:{}},{id:`run:${runId}`,kind:'run',issue_id:issueId,run_id:runId,title:'Run',status:'completed',created_at:'',details:{trigger_comment_id:commentId,trigger_content:secret,instructions:'allowed instructions'}},{id:`comment:${commentId}`,kind:'comment',issue_id:issueId,title:'Comment',status:'posted',created_at:'',details:{content:secret,comment_id:commentId}}],edges:[],findings:[],evidence:[],coverage:{complete:true,truncated:false,warnings:[]}};
const client=()=>new QueryClient({defaultOptions:{queries:{retry:false,gcTime:Infinity}}});
afterEach(()=>vi.restoreAllMocks());
it('reattaches only realtime listeners without reopening or closing caller-managed permission scope',()=>{
 const qc=client();openGraphScope(qc,scope);closeGraphScope(qc,scope);
 const first=attachGraphLifecycle(qc,scope,undefined,{manageScope:false});
 expect(isGraphScopeClosed(qc,scope)).toBe(true);first();
 openGraphScope(qc,scope);qc.setQueryData(graphKeys.issue(scope,issueId),graph);
 const second=attachGraphLifecycle(qc,scope,undefined,{manageScope:false});second();
 expect(isGraphScopeClosed(qc,scope)).toBe(false);expect(qc.getQueryData(graphKeys.issue(scope,issueId))).toEqual(graph);qc.clear();
});
it.each(['comment:deleted','comment:updated'] as const)('synchronously purges real %s payload and rejects a pending stale graph',async event=>{
 const qc=client();const events=new Map<WSEventType,(p:unknown)=>void>();
 const cleanup=attachGraphLifecycle(qc,scope,{subscribe:(type,fn)=>{events.set(type,fn);return()=>{};},onReconnect:()=>()=>{}});
 qc.setQueryData(graphKeys.issue(scope,issueId),graph);
 qc.setQueryData(graphKeys.messages(scope,runId),[{content:secret}]);
 let release!:(value:typeof graph)=>void;
 vi.spyOn(apiClient,'getIssueTrajectory').mockImplementation(()=>new Promise(resolve=>{release=resolve;}));
 const pending=qc.fetchQuery({...graphIssueOptions(scope,issueId),staleTime:0}).catch(()=>undefined);
 await vi.waitFor(()=>expect(release).toBeDefined());
 events.get(event)!(event==='comment:deleted'?{comment_id:commentId,issue_id:issueId}:{comment:{id:commentId,issue_id:issueId,deleted_at:'2026-11-19T05:00:00Z'}});
 expect(JSON.stringify(qc.getQueryCache().getAll().map(query=>query.state.data))).not.toContain(secret);
 expect(JSON.stringify(qc.getQueryData(graphKeys.issue(scope,issueId)))).toContain('allowed instructions');
 release(graph);await pending;
 expect(JSON.stringify(qc.getQueryData(graphKeys.issue(scope,issueId)))).not.toContain(secret);
 cleanup();qc.clear();
});
it('retains denial across failed refresh and future old responses, while preserving Run identity',async()=>{
 const qc=client();const events=new Map<WSEventType,(p:unknown)=>void>();const cleanup=attachGraphLifecycle(qc,scope,{subscribe:(type,fn)=>{events.set(type,fn);return()=>{};},onReconnect:()=>()=>{}});
 qc.setQueryData(graphKeys.issue(scope,issueId),graph);events.get('comment:deleted')!({comment_id:commentId,issue_id:issueId});
 vi.spyOn(apiClient,'getIssueTrajectory').mockRejectedValueOnce(Object.assign(new Error(secret),{status:503})).mockResolvedValue(graph);
 await expect(qc.fetchQuery({...graphIssueOptions(scope,issueId),staleTime:0})).rejects.toThrow('trajectory_read_failed');
 const refreshed=await qc.fetchQuery({...graphIssueOptions(scope,issueId),staleTime:0});
 expect(refreshed.nodes.find(n=>n.run_id===runId)?.details.trigger_content).toBeUndefined();expect(refreshed.nodes.find(n=>n.run_id===runId)?.id).toBe(`run:${runId}`);
 cleanup();qc.clear();
});
it('isolate keys and cancels/removes cached or pending content when scope closes',async()=>{
 const qc=client();qc.setQueryData(graphKeys.issue(scope,issueId),graph);
 let release!:(value:typeof graph)=>void;let requestSignal:AbortSignal|undefined;
 vi.spyOn(apiClient,'getIssueTrajectory').mockImplementation((_id,options)=>{requestSignal=options?.signal;return new Promise(resolve=>{release=resolve;});});
 const pending=qc.fetchQuery({...graphIssueOptions(scope,issueId),staleTime:0}).catch(()=>undefined);await vi.waitFor(()=>expect(release).toBeDefined());
 closeGraphScope(qc,scope);expect(requestSignal?.aborted).toBe(true);release(graph);await pending;
 expect(qc.getQueryData(graphKeys.issue(scope,issueId))).toBeUndefined();expect(graphKeys.all(scope)).not.toEqual(graphKeys.all({...scope,accountId:'other'}));qc.clear();
});
it('inherits native filters and uses100-row pages, signals and explicit scope',async()=>{
 const qc=client();const query={scope:{kind:'workspace' as const},filters:{},sort:{field:'created_at' as const,direction:'asc' as const}};
 const read=vi.spyOn(apiClient,'listIssueTableRows').mockResolvedValue({rows:[],next_cursor:null} as never);
 await qc.fetchInfiniteQuery(membershipOptions(scope,query));
 expect(read).toHaveBeenCalledWith(expect.objectContaining({query,group:{kind:'none'},hierarchy:{enabled:false},parent_id:null,page:{limit:100}}),expect.objectContaining({workspaceId:scope.workspaceId,signal:expect.any(AbortSignal)}));qc.clear();
});
it.each([403,404])('reports a%s membership failure to its live observer after removing all previous scope content',async status=>{
 const {QueryObserver}=await import('@tanstack/react-query');const qc=client();qc.setQueryData(graphKeys.issue(scope,issueId),graph);
 vi.spyOn(apiClient,'listIssueTableRows').mockRejectedValue(Object.assign(new Error(secret),{status}));
 const query={scope:{kind:'workspace' as const},filters:{},sort:{field:'created_at' as const,direction:'asc' as const}};
 const options=membershipOptions(scope,query);
 const observer=new QueryObserver(qc,{queryKey:options.queryKey,queryFn:({signal,client,queryKey})=>options.queryFn!({signal,client,queryKey,pageParam:null,direction:'forward',meta:undefined}),retry:false});
 let unavailable=false;const unsubscribe=observer.subscribe(result=>{if(result.error?.message==='trajectory_unavailable')unavailable=true;});
 await vi.waitFor(()=>expect(unavailable).toBe(true));expect(qc.getQueryData(graphKeys.issue(scope,issueId))).toBeUndefined();unsubscribe();qc.clear();
});
it('refreshes the open graph once per burst of workspace events and drops a pending refresh on detach',()=>{
 vi.useFakeTimers();
 try{
  const qc=client();const events=new Map<WSEventType,(p:unknown)=>void>();
  const invalidate=vi.spyOn(qc,'invalidateQueries');
  const cleanup=attachGraphLifecycle(qc,scope,{subscribe:(type,fn)=>{events.set(type,fn);return()=>{};},onReconnect:()=>()=>{}});
  for(const event of ['task:running','activity:created','comment:created','task:completed'] as const) events.get(event)!({workspace_id:scope.workspaceId,issue_id:issueId});
  expect(invalidate).not.toHaveBeenCalled();
  vi.advanceTimersByTime(GRAPH_EVENT_COALESCE_MS);
  expect(invalidate).toHaveBeenCalledTimes(1);
  events.get('issue:updated')!({issue:{id:issueId,workspace_id:scope.workspaceId}});
  cleanup();vi.advanceTimersByTime(GRAPH_EVENT_COALESCE_MS);
  expect(invalidate).toHaveBeenCalledTimes(1);qc.clear();
 }finally{vi.useRealTimers();}
});
