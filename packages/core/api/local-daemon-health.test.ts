// @vitest-environment node
import { describe, expect, it } from "vitest";
import { LocalDaemonHealthSchema } from "./schemas";

describe("local daemon compatibility and ownership",()=>{
 it("keeps an explicit default profile and tolerates an older daemon without an owner",()=>{
  expect(LocalDaemonHealthSchema.parse({status:"running",profile:""})).toMatchObject({status:"running",profile:""});
 });
 it("rejects malformed account identity instead of adopting it",()=>{
  expect(LocalDaemonHealthSchema.safeParse({status:"running",owner_id:42}).success).toBe(false);
 });
 it("does not turn unknown status or invalid counts into readiness or idleness",()=>{
  expect(LocalDaemonHealthSchema.parse({status:"future",active_task_count:-1})).toMatchObject({status:undefined,active_task_count:undefined});
 });
});
