import { mergeSubmission } from "@multica/core/local-conversation/submission";
import type { LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import type { MobileSubmission } from "./submission-outbox";

/** Canonical admission corrects the client's prediction without adding a second message. */
export function submissionPresentation(records:readonly MobileSubmission[],snapshot:LocalV2Snapshot|undefined){
 const body:MobileSubmission[]=[],queued:MobileSubmission[]=[];
 for(const record of records){
  const next=snapshot?.submissions.find((value)=>value.request_id===record.request.requestId);
  const authority=next?mergeSubmission(record.authority,next):record.authority;
  const native=authority?.attempt?.request_id??record.request.requestId;
  if(authority?.state==="removed"||(snapshot?.turns.some((value)=>value.request_id===native)&&authority?.state!=="queued"&&authority?.state!=="recovery_required"))continue;
  const value={...record,authority};
  if(authority?.state==="queued"||authority?.state==="recovery_required"||(!authority&&record.request.queued))queued.push(value);
  else if(authority?.state==="starting"||!authority)body.push(value);
 }
 queued.sort((a,b)=>(a.position??Number.MAX_SAFE_INTEGER)-(b.position??Number.MAX_SAFE_INTEGER));
 return {body,queued};
}
