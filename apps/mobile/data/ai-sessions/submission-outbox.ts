import { z } from "zod";
import { create } from "zustand";
import { localSubmissionSchema, mergeSubmission, type LocalSubmission } from "@multica/core/local-conversation/submission";
import { readStoredSubmissions, writeStoredSubmissions } from "./files";

const requestSchema=z.object({requestId:z.string().min(1),text:z.string(),attachments:z.array(z.string()),queued:z.boolean().optional(),queuePaused:z.boolean().optional(),queueContent:z.string().optional(),createdAt:z.number(),prepared:z.object({text:z.string(),prompt:z.string(),references:z.array(z.object({kind:z.enum(["session","issue","agent"]),id:z.string()})),agents:z.array(z.object({id:z.string(),name:z.string()}))}).optional()});
const recordSchema=z.object({assets:z.array(z.object({id:z.string(),uri:z.string(),name:z.string(),mimeType:z.string().nullable().optional()})).optional(),request:requestSchema,body:z.record(z.string(),z.unknown()),generation:z.string(),lifecycle:z.boolean(),confirmed:z.boolean(),authority:localSubmissionSchema.optional()});
export type MobileSubmission=z.infer<typeof recordSchema>;
export const EMPTY_SUBMISSIONS:readonly MobileSubmission[]=[];
export const useSubmissionOutbox=create<{records:Record<string,readonly MobileSubmission[]>}>(()=>({records:{}}));
interface Disk {read(key:string):Promise<string|null>;write(key:string,value:string):Promise<void>}
/** Each namespace is atomically replaced, and its writes are serialized before publishing. */
export function createSubmissionOutbox(disk:Disk={read:readStoredSubmissions,write:writeStoredSubmissions}){
 const pending=new Map<string,Promise<unknown>>();const loaded=new Set<string>();
 const serial=<T>(key:string,fn:()=>Promise<T>):Promise<T>=>{const run=(pending.get(key)??Promise.resolve()).catch(()=>{}).then(fn);pending.set(key,run);return run;};
 const publish=(key:string,records:readonly MobileSubmission[])=>useSubmissionOutbox.setState((state)=>({records:{...state.records,[key]:records}}));
 const read=async(key:string)=>{if(loaded.has(key))return;const raw=await disk.read(key);const records=raw?z.array(recordSchema).parse(JSON.parse(raw)):[];publish(key,records);loaded.add(key);};
 const mutate=(key:string,fn:(records:readonly MobileSubmission[])=>readonly MobileSubmission[])=>serial(key,async()=>{await read(key);const records=fn(useSubmissionOutbox.getState().records[key]??EMPTY_SUBMISSIONS);await disk.write(key,JSON.stringify(records));publish(key,records);});
 return {
  load:(key:string)=>serial(key,()=>read(key)),
  put:(key:string,value:MobileSubmission)=>mutate(key,(records)=>{const next=recordSchema.parse(value);const previous=records.find((record)=>record.request.requestId===next.request.requestId);if(previous)return records.map((record)=>record===previous?{...next,confirmed:previous.confirmed||next.confirmed,authority:next.authority?mergeSubmission(previous.authority,next.authority):previous.authority}:record);return [...records,next];}),
  confirm:(key:string,id:string,authority?:LocalSubmission)=>mutate(key,(records)=>records.map((record)=>record.request.requestId===id?{...record,confirmed:true,authority:authority?mergeSubmission(record.authority,authority):record.authority}:record)),
  remove:(key:string,id:string)=>mutate(key,(records)=>records.filter((record)=>record.request.requestId!==id)),
  mirror:(key:string,values:MobileSubmission[])=>mutate(key,(records)=>{const merged=new Map(records.map((record)=>[record.request.requestId,record]));for(const value of values){const previous=merged.get(value.request.requestId);merged.set(value.request.requestId,{...value,authority:value.authority?mergeSubmission(previous?.authority,value.authority):previous?.authority});}return [...merged.values()];}),
 };
}
export const submissionOutbox=createSubmissionOutbox();
