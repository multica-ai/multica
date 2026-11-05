// @vitest-environment node
import {describe,it,expect} from "vitest";
import {GraphCredentialsSchema,GraphReadableSchema,GraphAuthSavedSchema} from "./codegraph-auth";
import {CodeGraphRepositorySchema} from "./schemas";
describe("Git credential API boundary",()=>{
 it("rejects malformed management responses instead of claiming credentials exist",()=>{
  expect(GraphCredentialsSchema.safeParse({items:[{id:"",name:"x"}],configured:true}).success).toBe(false);
  expect(GraphCredentialsSchema.safeParse({items:[],configured:"true"}).success).toBe(false);
 });
 it("requires explicit verification and save acknowledgements",()=>{
  expect(GraphReadableSchema.safeParse({readable:false}).success).toBe(false);
  expect(GraphAuthSavedSchema.safeParse({saved:"true"}).success).toBe(false);
 });
 it("preserves old graph payloads and handles unknown authentication modes",()=>{
  expect(CodeGraphRepositorySchema.parse({id:"old"}).auth_mode).toBeUndefined();
  expect(CodeGraphRepositorySchema.parse({id:"new",auth_mode:"future",credential_id:null}).auth_mode).toBeUndefined();
 });
});
