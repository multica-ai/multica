// @vitest-environment node
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { afterEach, expect, it, vi } from "vitest";
import type { Session } from "electron";
import type { LocalV2Client } from "./local-conversation-v2";
import type { PreviewProxyOptions } from "./preview-proxy";

const fake = vi.hoisted(() => ({ proxies: [] as Array<{ options: PreviewProxyOptions; close: ReturnType<typeof vi.fn> }> }));
vi.mock("electron", async () => { const { EventEmitter } = await import("node:events"); return { app: new EventEmitter(), session: { fromPartition: () => ({ resolveProxy: vi.fn(async () => "DIRECT") }) } }; });
vi.mock("./preview-proxy", () => ({ createPreviewProxy: async (options: PreviewProxyOptions) => { const value = { options, address: `127.0.0.1:${40000 + fake.proxies.length}`, username: "user", password: "password", close: vi.fn() }; fake.proxies.push(value); return value; } }));
import { app } from "electron";
import { RemotePreviewNetwork } from "./remote-preview-network";

const networks: RemotePreviewNetwork[] = [];
afterEach(() => { networks.splice(0).forEach((value) => value.dispose()); fake.proxies.length = 0; vi.useRealTimers(); });
const owner = JSON.stringify(["user", "workspace"]), target = { deviceId: "peer", conversationId: "conversation" };
const lease = { version: 1, service_id: "1".repeat(64), capability: "a".repeat(64), expires_at: new Date(Date.now() + 1800_000).toISOString(), protocol: "/multica/preview-tcp/1" };
function setup() {
  const request = vi.fn(async (_scope, method, path) => path === "/hello" ? { capabilities: ["remote-service-preview-v1"] } : method === "DELETE" ? { closed: true } : lease);
  const client = { request, previewTunnel: vi.fn(async () => new PassThrough()) };
  const network = new RemotePreviewNetwork(client as unknown as LocalV2Client, "https://api.example.test", () => "user"); networks.push(network);
  const ses = { closeAllConnections: vi.fn(async () => {}), setProxy: vi.fn(async () => {}) } as unknown as Session;
  return { network, client, ses };
}
it("binds the source peer, scope and port before browser traffic and retains the proxy while a tab is hidden", async () => {
  const { network, client, ses } = setup();
  const release = await network.acquire(owner, target, "http://localhost:3000/login", ses, () => {});
  expect(ses.setProxy).toHaveBeenCalledWith({ mode: "fixed_servers", proxyRules: "http://127.0.0.1:40000", proxyBypassRules: "<-loopback>" });
  expect(client.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "POST", "/preview/services", expect.objectContaining({ conversation_id: "conversation", target: { host: "localhost", port: 3000 } }), "peer");
  await fake.proxies[0]!.options.connect({ host: "localhost", port: 3000, url: "http://localhost:3000/" }, new AbortController().signal);
  expect(client.previewTunnel).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "peer", lease.service_id, lease.capability, expect.any(AbortSignal));
  const other = await network.acquire(owner, target, "http://localhost:3000/", ses, () => {});
  release(); expect(fake.proxies[0]!.close).not.toHaveBeenCalled(); other(); expect(fake.proxies[0]!.close).toHaveBeenCalledOnce();
  await expect(fake.proxies[0]!.options.connect({ host: "localhost", port: 3000, url: "http://localhost:3000/" }, new AbortController().signal)).rejects.toThrow("context has closed");
});
it("isolates devices and source conversations while keeping public-only previews independent of remote support", async () => {
  const { network, client, ses } = setup();
  expect(network.partition(owner, target)).not.toBe(network.partition(owner, { ...target, deviceId: "other" }));
  expect(network.partition(owner, target)).not.toBe(network.partition(owner, { ...target, conversationId: "other" }));
  const release = await network.acquire(owner, target, "https://example.test/", ses, () => {});
  expect(client.request).not.toHaveBeenCalled(); release();
});
it("fails closed on old devices without opening a tunnel or falling back to this machine", async () => {
  const { network, client, ses } = setup(); client.request.mockResolvedValueOnce({ capabilities: [] });
  await expect(network.acquire(owner, target, "http://localhost:3000/", ses, () => {})).rejects.toThrow("preview_unsupported");
  expect(client.previewTunnel).not.toHaveBeenCalled(); expect(fake.proxies[0]!.close).toHaveBeenCalledOnce();
});
it("revokes a capability arriving after the preview context was closed", async () => {
  const { network, client, ses } = setup(); let resolve!: (value: typeof lease) => void;
  client.request.mockImplementation(async (_scope, method, path) => path === "/hello" ? { capabilities: ["remote-service-preview-v1"] } : method === "POST" ? new Promise((done) => { resolve = done; }) : { closed: true });
  const pending = network.acquire(owner, target, "http://localhost:3000/", ses, () => {}); const rejected = expect(pending).rejects.toThrow("context closed");
  await vi.waitFor(() => expect(resolve).toBeTypeOf("function")); network.dispose(); resolve(lease); await rejected;
  expect(client.request).toHaveBeenCalledWith(expect.anything(), "DELETE", `/preview/services/${lease.service_id}`, undefined, "peer");
});
it("supplies proxy credentials only to the bound browser session and proxy endpoint", async () => {
  const { network, ses } = setup(); await network.acquire(owner, target, "http://localhost:3000/", ses, () => {});
  const callback = vi.fn(), event = { preventDefault: vi.fn() };
  const emitter = app as unknown as EventEmitter;
  emitter.emit("login", event, { session: ses }, {}, { isProxy: true, host: "127.0.0.1", port: 40000 }, callback);
  expect(callback).toHaveBeenCalledWith("user", "password"); callback.mockClear();
  emitter.emit("login", event, { session: {} }, {}, { isProxy: true, host: "127.0.0.1", port: 40000 }, callback);
  emitter.emit("login", event, { session: ses }, {}, { isProxy: false, host: "127.0.0.1", port: 40000 }, callback);
  emitter.emit("login", event, null, {}, { isProxy: true, host: "127.0.0.1", port: 40000 }, callback);
  expect(callback).not.toHaveBeenCalled();
});

it("rejects malformed grants before opening a data stream", async () => {
  const { network, client, ses } = setup(); client.request.mockResolvedValueOnce({ capabilities: ["remote-service-preview-v1"] }).mockResolvedValueOnce({ ...lease, capability: "invalid" });
  await expect(network.acquire(owner, target, "http://localhost:3000/", ses, () => {})).rejects.toThrow("preview_unsupported");
  expect(client.previewTunnel).not.toHaveBeenCalled();
});

it("replaces an expired grant once before sending any webpage bytes", async () => {
  const { network, client, ses } = setup(); await network.acquire(owner, target, "http://localhost:3000/", ses, () => {});
  client.previewTunnel.mockRejectedValueOnce(new Error("service_expired: Lease expired"));
  const stream = await fake.proxies[0]!.options.connect({ host: "localhost", port: 3000, url: "http://localhost:3000/" }, new AbortController().signal);
  expect(client.previewTunnel).toHaveBeenCalledTimes(2); expect(client.request.mock.calls.filter(([, method, path]) => method === "POST" && path === "/preview/services")).toHaveLength(2); stream.destroy();
});

it("renews open preview grants independently of window focus and stops after the last reference closes", async () => {
  vi.useFakeTimers(); const { network, client, ses } = setup();
  const release = await network.acquire(owner, target, "http://localhost:3000/", ses, () => {});
  await vi.advanceTimersByTimeAsync(10 * 60_000);
  expect(client.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "POST", `/preview/services/${lease.service_id}/renew`, {}, "peer");
  release(); const count = client.request.mock.calls.filter(([, , path]) => path.endsWith("/renew")).length;
  await vi.advanceTimersByTimeAsync(10 * 60_000);
  expect(client.request.mock.calls.filter(([, , path]) => path.endsWith("/renew"))).toHaveLength(count);
});

it.each(["192.168.1.10", "8.8.8.8", "0.0.0.0", "[2001:db8::10]"])("opens an IP service on the source device without rewriting %s", async (host) => {
  const { network, client, ses } = setup();
  const url = `http://${host}:3000/path`;
  await network.acquire(owner, target, url, ses, () => {});
  expect(client.request).toHaveBeenCalledWith(expect.anything(), "POST", "/preview/services", expect.objectContaining({ target: { host: host.replace(/^\[|\]$/g, ""), port: 3000 } }), "peer");
  const stream = await fake.proxies[0]!.options.connect({ host: host.replace(/^\[|\]$/g, ""), port: 3000, url }, new AbortController().signal);
  expect(client.previewTunnel).toHaveBeenCalledOnce(); stream.destroy();
});
