import { z } from "zod";
import { localSubmissionResultSchema } from "@multica/core/local-conversation/v2";
import type { AISessionClient } from "./client";
import { submissionOutbox,type MobileSubmission } from "./submission-outbox";
import { draftUploads } from "./uploads";
import { queueSchema } from "./schemas";

/** Upload recovery and delivery always use the frozen, durably stored request. */
export async function deliverSubmission(client:AISessionClient,session:string,key:string,draftKey:string,record:MobileSubmission,assertCurrent:()=>void){
 assertCurrent();
 draftUploads.restore(client,draftKey,record.assets??[]);
 const ids=await draftUploads.ensure(record.request.attachments);
 if(ids.some((value,index)=>value!==record.request.attachments[index])){
  const item=z.record(z.string(),z.unknown()).safeParse(record.body.item);
  record={...record,request:{...record.request,attachments:ids},body:item.success?{...record.body,item:{...item.data,attachmentIds:ids}}:{...record.body,attachment_ids:ids}};
  await submissionOutbox.put(key,record);
 }
 assertCurrent();
 const result=record.lifecycle?await client.submit(session,record.body,localSubmissionResultSchema,record.request.createdAt):await client.submit(session,record.body,queueSchema,record.request.createdAt);
 await submissionOutbox.confirm(key,record.request.requestId,"submission" in result?result.submission:undefined);
 draftUploads.forget(record.assets?.map((asset)=>asset.id)??record.request.attachments);
 return result;
}
