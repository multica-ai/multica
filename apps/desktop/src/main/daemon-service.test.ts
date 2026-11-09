// @vitest-environment node
import { describe, expect, it } from "vitest";
import { backendServiceURL, parseServiceRecord, probeBackendService, serviceProfileArgs, isSharedExecutionService, desktopAuthProfile } from "./daemon-service";
import { handoffConfirmationOptions, sharedStopConfirmationOptions } from "./daemon-handoff-dialogs";

const record = { server_url:"https://example.test",owner_id:"user-a",profile:"",daemon_id:"machine",pid:42,port:19514 };
const health = {...record,status:"running"};
const read = async () => JSON.stringify(record);

describe("backend service discovery",()=>{
 it.each([
  ["https://EXAMPLE.test:443/","https://example.test"],
  ["wss://example.test/ws","https://example.test"],
  ["http://example.test:80/","http://example.test"],
  ["HTTPS://example.test/a/../b","https://example.test/b"],
  ["https://example.test/%61","https://example.test/a"],
  ["https://example.test/任务","https://example.test/%E4%BB%BB%E5%8A%A1"],
  ["https://example.test/a!","https://example.test/a%21"],
 ])("normalizes %s",(input,expected)=>expect(backendServiceURL(input)).toBe(expected));
 it("preserves deployment paths and rejects embedded credentials",()=>{
  expect(backendServiceURL("https://example.test/a")).not.toBe(backendServiceURL("https://example.test/b"));
  expect(()=>backendServiceURL("https://user:secret@example.test")).toThrow();
 });
 it("accepts the default CLI profile and checks live process identity",async()=>{
  await expect(probeBackendService(record.server_url,async()=>health,read)).resolves.toEqual(health);
 });
 it.each(["pid","profile","owner_id","daemon_id","server_url"])("rejects a stale or foreign %s",async(field)=>{
  const changed={...health,[field]:field==="pid"?43:"other"};
  await expect(probeBackendService(record.server_url,async()=>changed,read)).rejects.toThrow();
 });
 it("ignores a stale record only when its process is no longer alive",async()=>{
  await expect(probeBackendService(record.server_url,async()=>null,read)).resolves.toBeNull();
 });
 it("does not hide malformed or unreadable records",async()=>{
  expect(()=>parseServiceRecord('{}')).toThrow();
  await expect(probeBackendService(record.server_url,async()=>health,async()=>{throw Object.assign(new Error('denied'),{code:'EACCES'});})).rejects.toThrow('denied');
 });
});

describe("explicit service handoff",()=>{
 it.each(["en","zh-CN","zh-TW","ja","ko","fr","es"])("defaults to cancel in %s",(locale)=>{
  const options=handoffConfirmationOptions(locale,true);
  expect(options.defaultId).toBe(0);expect(options.cancelId).toBe(0);
  expect(options.buttons).toHaveLength(3);expect(options.detail).toBeTruthy();
  expect(sharedStopConfirmationOptions(locale).buttons).toHaveLength(3);
 });
});


describe("observed execution service",()=>{
 it("keeps an explicit default profile argument instead of falling back",()=>{
  expect(serviceProfileArgs("")).toEqual(["--profile",""]);
  expect(()=>serviceProfileArgs("../outside")).toThrow();
 });
 it("treats a CLI launch as shared even when the profile happens to match",()=>{
  expect(isSharedExecutionService({profile:"desktop-a",owner_id:"a"},"desktop-a")).toBe(true);
  expect(isSharedExecutionService({profile:"desktop-a",owner_id:"a",launched_by:"desktop"},"desktop-a")).toBe(false);
 });
});


describe("backend auth configuration isolation",()=>{
 it("keeps existing profiles but separates deployments and protocols sharing a host",()=>{
  expect(desktopAuthProfile("https://example.test/","desktop-example","https://example.test")).toBe("desktop-example");
  const a=desktopAuthProfile("https://example.test/a","desktop-example","https://example.test");
  const b=desktopAuthProfile("https://example.test/b","desktop-example","https://example.test");
  expect(a).not.toBe(b);expect(a).not.toBe("desktop-example");
  expect(desktopAuthProfile("http://example.test","desktop-example","https://example.test")).not.toBe("desktop-example");
 });
});
