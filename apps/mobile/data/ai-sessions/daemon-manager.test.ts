// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { EmbeddedDaemonManager, networkSignature } from "./daemon-manager";

const configuration = { userId: "account-a", token: "login-a", apiUrl: "https://api.test", registryUrl: "https://registry.test", deviceName: "iPhone" };
const endpoint = JSON.stringify({ url: "http://127.0.0.1:3456", token: "private-loopback" });
afterEach(() => vi.useRealTimers());
const lifecycle = () => ({ updateToken: vi.fn(async (_token: string) => {}), networkChanged: vi.fn(async () => {}), enterBackground: vi.fn(async () => {}), enterForeground: vi.fn(async () => true) });
const running = () => ({ start: vi.fn(async (_configuration: string) => endpoint), stop: vi.fn(async () => {}), ...lifecycle() });

it("starts independently of screens, reports readiness and shuts down its endpoint", async () => {
  const native = running();
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  expect(native.start).toHaveBeenCalledWith(JSON.stringify(configuration));
  expect(manager.snapshot()).toMatchObject({ status: "ready", endpoint: { token: "private-loopback" } });
  const stopping = manager.stop();
  expect(manager.snapshot()).toMatchObject({ status: "stopped", endpoint: null });
  await stopping;
});

it("does not publish credentials from a start completed after logout", async () => {
  let resolve!: (endpoint: string) => void;
  const native = { ...running(), start: vi.fn(() => new Promise<string>((done) => { resolve = done; })) };
  const manager = new EmbeddedDaemonManager(native);
  const starting = manager.configure(configuration);
  await vi.waitFor(() => expect(native.start).toHaveBeenCalledOnce());
  const stopping = manager.stop();
  resolve(endpoint);
  await Promise.all([starting, stopping]);
  expect(manager.snapshot()).toEqual({ status: "stopped", endpoint: null, error: null });
  expect(native.stop.mock.calls.length).toBeGreaterThanOrEqual(2);
});

it("serializes account replacement and never starts a stale queued account", async () => {
  const native = running();
  const manager = new EmbeddedDaemonManager(native);
  await Promise.all([manager.configure(configuration), manager.configure({ ...configuration, userId: "account-b", token: "login-b" })]);
  expect(native.start).toHaveBeenCalledOnce();
  expect(JSON.parse(native.start.mock.calls[0][0])).toMatchObject({ userId: "account-b", token: "login-b" });
  await manager.stop();
});

it("restarts after a failed health probe and cancels probes on background stop", async () => {
  vi.useFakeTimers();
  const native = running();
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 503 }));
  const manager = new EmbeddedDaemonManager(native, fetcher);
  await manager.configure(configuration);
  await vi.advanceTimersByTimeAsync(15000);
  expect(fetcher).toHaveBeenCalledWith("http://127.0.0.1:3456/health", expect.objectContaining({ headers: { Authorization: "Bearer private-loopback" } }));
  expect(native.start).toHaveBeenCalledTimes(2);
  await manager.stop();
  await vi.advanceTimersByTimeAsync(60000);
  expect(native.start).toHaveBeenCalledTimes(2);
});

it("backs off failed native starts and stops retrying when the app stops", async () => {
  vi.useFakeTimers();
  const native = { ...running(), start: vi.fn(async (): Promise<string> => { throw new Error("network"); }) };
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  expect(manager.snapshot()).toMatchObject({ status: "failed", error: "connection_failed" });
  await vi.advanceTimersByTimeAsync(1000);
  expect(native.start).toHaveBeenCalledTimes(2);
  await manager.stop(); await vi.advanceTimersByTimeAsync(30000);
  expect(native.start).toHaveBeenCalledTimes(2);
});

it("shows a missing native module explicitly and rejects non-loopback endpoints", async () => {
  const absent = new EmbeddedDaemonManager(null);
  await absent.configure(configuration);
  expect(absent.snapshot()).toMatchObject({ error: "native_unavailable" });
  const native = { ...running(), start: vi.fn(async () => JSON.stringify({ url: "https://untrusted.test", token: "secret" })) };
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  expect(manager.snapshot().endpoint).toBeNull();
  await manager.stop(); await absent.stop();
});

it("updates a refreshed token in place, keeping the endpoint and its connections", async () => {
  const native = running();
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  const before = manager.snapshot();
  await manager.configure(configuration);
  await manager.configure({ ...configuration, token: "login-refreshed" });
  expect(native.updateToken.mock.calls).toEqual([["login-refreshed"]]);
  expect(native.start).toHaveBeenCalledOnce();
  expect(manager.snapshot()).toBe(before);
  await manager.stop();
});

it("restarts when another account signs in, even from a running link", async () => {
  const native = running();
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  await manager.configure({ ...configuration, userId: "account-b", token: "login-b" });
  expect(native.updateToken).not.toHaveBeenCalled();
  expect(native.start).toHaveBeenCalledTimes(2);
  await manager.stop();
});

it("falls back to a restart when the token cannot be updated in place", async () => {
  const native = running();
  native.updateToken.mockRejectedValueOnce(new Error("native"));
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  await manager.configure({ ...configuration, token: "login-refreshed" });
  await vi.waitFor(() => expect(native.start).toHaveBeenCalledTimes(2));
  expect(JSON.parse(native.start.mock.calls[1][0])).toMatchObject({ token: "login-refreshed" });
  await manager.stop();
});

it("leaves the link running in the background, pauses health probes, and checks it at once on return", async () => {
  vi.useFakeTimers();
  const native = running();
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 503 }));
  const manager = new EmbeddedDaemonManager(native, fetcher);
  await manager.configure(configuration);
  const before = manager.snapshot();
  await manager.setForeground(false);
  expect(native.enterBackground).toHaveBeenCalledOnce();
  await vi.advanceTimersByTimeAsync(60_000);
  expect(fetcher).not.toHaveBeenCalled();
  await manager.setForeground(true);
  expect(native.enterForeground).toHaveBeenCalledOnce();
  expect(native.start).toHaveBeenCalledOnce();
  expect(manager.snapshot()).toBe(before);
  fetcher.mockResolvedValue(new Response(null, { status: 200 }));
  await vi.advanceTimersByTimeAsync(0);
  expect(fetcher).toHaveBeenCalledOnce();
  await vi.advanceTimersByTimeAsync(15_000);
  expect(fetcher).toHaveBeenCalledTimes(2);
  await manager.stop();
});

it("restarts on return when the link is no longer running", async () => {
  const native = running();
  native.enterForeground.mockResolvedValueOnce(false);
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  await manager.setForeground(false);
  await manager.setForeground(true);
  await vi.waitFor(() => expect(native.start).toHaveBeenCalledTimes(2));
  await manager.stop();
});

it("uses the refreshed token when returning to a link that stopped", async () => {
  const native = running();
  native.enterForeground.mockResolvedValueOnce(false);
  const manager = new EmbeddedDaemonManager(native);
  await manager.configure(configuration);
  await manager.setForeground(false);
  await manager.configure({ ...configuration, token: "login-refreshed" });
  await manager.setForeground(true);
  await vi.waitFor(() => expect(native.start).toHaveBeenCalledTimes(2));
  expect(JSON.parse(native.start.mock.calls[1][0])).toMatchObject({ token: "login-refreshed" });
  await manager.stop();
});

it("does not start in the background, and stops immediately on logout there", async () => {
  const native = running();
  const manager = new EmbeddedDaemonManager(native);
  await manager.setForeground(false);
  await manager.configure(configuration);
  expect(native.start).not.toHaveBeenCalled();
  await manager.setForeground(true);
  await vi.waitFor(() => expect(native.start).toHaveBeenCalledOnce());
  await manager.setForeground(false);
  await manager.stop();
  expect(manager.snapshot()).toEqual({ status: "stopped", endpoint: null, error: null });
  await manager.setForeground(true);
  expect(native.start).toHaveBeenCalledOnce();
});

it("tells a ready link about network changes and only when the network identity changed", async () => {
  const native = running();
  const manager = new EmbeddedDaemonManager(native);
  await manager.networkChanged();
  expect(native.networkChanged).not.toHaveBeenCalled();
  await manager.configure(configuration);
  await manager.networkChanged();
  expect(native.networkChanged).toHaveBeenCalledOnce();
  await manager.stop();

  const wifi = { type: "wifi", isConnected: true, details: { ipAddress: "192.168.1.2", subnet: "255.255.255.0", isConnectionExpensive: false } };
  expect(networkSignature({ ...wifi })).toBe(networkSignature(wifi));
  expect(networkSignature({ ...wifi, details: { ...wifi.details, ipAddress: "192.168.1.3" } })).not.toBe(networkSignature(wifi));
  expect(networkSignature({ type: "cellular", isConnected: true, details: { carrier: "A", cellularGeneration: "5g" } })).not.toBe(networkSignature(wifi));
});
