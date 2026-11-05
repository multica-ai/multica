// @vitest-environment node
import {expect,it} from "vitest";
import {localV2SnapshotSchema} from "@multica/core/local-conversation/v2";
import {localSubmissionSchema} from "@multica/core/local-conversation/submission";
import {submissionPresentation} from "./submission-presentation";
import type {MobileSubmission} from "./submission-outbox";
const record:MobileSubmission={request:{requestId:"r",text:"hello",attachments:[],queued:false,createdAt:1},body:{},generation:"g",lifecycle:true,confirmed:false};
const snapshot=localV2SnapshotSchema.parse({version:2,store_generation:"g",snapshot_seq:1,conversation:{conversation_id:"s",provider:"claude",title:"",summary_seq:1,status:"idle",pinned:false,archived:false,updated_at:"2026-11-05T00:00:00Z"},turns:[]});
it("corrects optimistic body to queue from authoritative admission, then merges the formal message",()=>{
 expect(submissionPresentation([record],snapshot).body).toHaveLength(1);
 const queued=localSubmissionSchema.parse({request_id:"r",revision:2,state:"queued",attempt:null});
 expect(submissionPresentation([record],{...snapshot,submissions:[queued]})).toMatchObject({body:[],queued:[{request:{requestId:"r"}}]});
 const started=localSubmissionSchema.parse({...queued,revision:3,state:"starting",attempt:{attempt_id:"a",request_id:"r-retry-1",turn_ids:[],delivery:"not_sent"}});
 expect(submissionPresentation([record],{...snapshot,submissions:[started]}).body).toHaveLength(1);
 expect(submissionPresentation([record],{...snapshot,submissions:[started],turns:[{id:"t",turn_index:0,request_id:"r-retry-1",status:"running",started_at:"2026-11-05T00:00:00Z",messages:[],events:[]}]})).toEqual({body:[],queued:[]});
});

it("retains queued retries beside an older failed formal turn and restores daemon queue order",()=>{
 const authority=localSubmissionSchema.parse({request_id:"r",revision:5,state:"recovery_required",attempt:{attempt_id:"a",request_id:"r",turn_ids:["failed"],delivery:"unknown"}});
 const failed={...snapshot,turns:[{id:"failed",turn_index:0,request_id:"r",status:"failed",started_at:"2026-11-05T00:00:00Z",messages:[],events:[]}]};
 expect(submissionPresentation([{...record,confirmed:true,authority}],failed).queued).toHaveLength(1);
 const other={...record,request:{...record.request,requestId:"other",queued:true},position:0};
 expect(submissionPresentation([{...record,request:{...record.request,queued:true},position:1},other],snapshot).queued.map((value)=>value.request.requestId)).toEqual(["other","r"]);
});
