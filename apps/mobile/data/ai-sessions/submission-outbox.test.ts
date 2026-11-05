// @vitest-environment node
import { expect,it,vi,beforeEach } from "vitest";
import {createSubmissionOutbox,useSubmissionOutbox,type MobileSubmission} from "./submission-outbox";
vi.mock("./files",()=>({readStoredSubmissions:vi.fn(),writeStoredSubmissions:vi.fn()}));
const record=(id:string):MobileSubmission=>({request:{requestId:id,text:id,attachments:[],createdAt:1},body:{type:"submit",item:{requestId:id}},generation:"g",lifecycle:true,confirmed:false});
beforeEach(()=>useSubmissionOutbox.setState({records:{}}));
it("publishes only after a durable write and restores all requests after restarting",async()=>{
 const files=new Map<string,string>();let complete!:(value?:unknown)=>void;
 const disk={read:async(key:string)=>files.get(key)??null,write:vi.fn(async(key:string,value:string)=>{await new Promise((resolve)=>{complete=resolve;});files.set(key,value);})};
 const first=createSubmissionOutbox(disk);const save=first.put("account/device/conversation",record("a"));
 await vi.waitFor(()=>expect(disk.write).toHaveBeenCalledOnce());expect(useSubmissionOutbox.getState().records["account/device/conversation"]).toEqual([]);
 complete();await save;const restarted=createSubmissionOutbox({read:disk.read,write:async(key,value)=>{files.set(key,value);}});
 await restarted.load("account/device/conversation");expect(useSubmissionOutbox.getState().records["account/device/conversation"]?.[0].request.requestId).toBe("a");
 await restarted.put("account/device/conversation",record("b"));await restarted.confirm("account/device/conversation","a");
 expect(useSubmissionOutbox.getState().records["account/device/conversation"]?.map((value)=>[value.request.requestId,value.confirmed])).toEqual([["a",true],["b",false]]);
 await restarted.load("other-account/device/conversation");expect(useSubmissionOutbox.getState().records["other-account/device/conversation"]).toEqual([]);
});
it("leaves the message out of the accepted outbox if the disk fails",async()=>{
 const box=createSubmissionOutbox({read:async()=>null,write:async()=>{throw new Error("disk full");}});
 await expect(box.put("scope",record("a"))).rejects.toThrow("disk full");expect(useSubmissionOutbox.getState().records.scope).toEqual([]);
});
