// @vitest-environment node
import http from "node:http";
import net from "node:net";
import { once } from "node:events";
import { afterEach, expect, it, vi } from "vitest";
const fake = vi.hoisted(() => ({ lookup: vi.fn(async () => [{ address: "8.8.8.8", family: 4 }]) }));
vi.mock("node:dns/promises", () => ({ lookup: fake.lookup }));
import { connectPreviewEgress } from "./preview-egress";
afterEach(() => { fake.lookup.mockReset(); fake.lookup.mockResolvedValue([{ address: "8.8.8.8", family: 4 }]); });
it.each(["127.0.0.1", "192.168.1.10", "8.8.8.8", "::1", "localhost"])("keeps explicit service host %s out of the webpage egress", async (host) => {
  const resolve = vi.fn();
  await expect(connectPreviewEgress({ host, port: 80, url: `http://${host.includes(":") ? `[${host}]` : host}/` }, new AbortController().signal, resolve)).rejects.toThrow("preview_destination_denied");
  expect(resolve).not.toHaveBeenCalled(); expect(fake.lookup).not.toHaveBeenCalled();
});
it("uses a system HTTP proxy without resolving a Fake-IP locally and preserving buffered tunnel bytes", async () => {
  fake.lookup.mockResolvedValue([{ address: "198.18.0.235", family: 4 }]);
  const server = http.createServer(); let authority = "";
  server.on("connect", (request, socket) => { authority = request.url!; socket.write("HTTP/1.1 200 Connection Established\r\n\r\nREADY"); });
  server.listen(0, "127.0.0.1"); await once(server, "listening"); const address = server.address() as net.AddressInfo;
  const controller = new AbortController();
  try {
    const stream = await connectPreviewEgress({ host: "example.test", port: 443, url: "https://example.test/" }, controller.signal, async () => `PROXY 127.0.0.1:${address.port}`);
    expect(authority).toBe("example.test:443"); expect(fake.lookup).not.toHaveBeenCalled(); stream.resume(); const [data] = await once(stream, "data"); expect(data.toString()).toBe("READY"); stream.destroy();
  } finally { controller.abort(); server.closeAllConnections(); server.close(); }
});

it.each(["SOCKS4", "SOCKS5"])("uses the system %s proxy with the domain destination and buffered response", async (kind) => {
  let received = Buffer.alloc(0), greeted = kind === "SOCKS4";
  const sockets = new Set<net.Socket>();
  const server = net.createServer((socket) => {
    sockets.add(socket); socket.once("close", () => sockets.delete(socket));
    socket.on("data", (data: Buffer) => {
      if (!greeted) { expect(data).toEqual(Buffer.from([5, 1, 0])); greeted = true; socket.write(Buffer.from([5, 0])); return; }
      received = Buffer.from(data);
      socket.write(Buffer.concat([kind === "SOCKS4" ? Buffer.from([0, 90, 0, 0, 0, 0, 0, 0]) : Buffer.from([5, 0, 0, 1, 0, 0, 0, 0, 0, 0]), Buffer.from("READY")]));
    });
  });
  server.listen(0, "127.0.0.1"); await once(server, "listening"); const address = server.address() as net.AddressInfo;
  const controller = new AbortController();
  try {
    const stream = await connectPreviewEgress({ host: "example.test", port: 443, url: "https://example.test/" }, controller.signal, async () => `${kind} 127.0.0.1:${address.port}`);
    expect(fake.lookup).not.toHaveBeenCalled();
    expect(received).toEqual(kind === "SOCKS4" ? Buffer.concat([Buffer.from([4, 1, 1, 187, 0, 0, 0, 1, 0]), Buffer.from("example.test\0")]) : Buffer.concat([Buffer.from([5, 1, 0, 3, 12]), Buffer.from("example.test"), Buffer.from([1, 187])]));
    const response = once(stream, "data"); stream.resume(); expect((await response)[0].toString()).toBe("READY"); stream.destroy();
  } finally { controller.abort(); for (const socket of sockets) socket.destroy(); server.close(); }
});

it("uses the system DNS result on DIRECT for a domain without reclassifying it as a service", async () => {
  const server = net.createServer((socket) => { socket.end("DIRECT"); });
  server.listen(0, "127.0.0.1"); await once(server, "listening");
  fake.lookup.mockResolvedValue([{ address: "127.0.0.1", family: 4 }]);
  try {
    const stream = await connectPreviewEgress({ host: "example.test", port: (server.address() as net.AddressInfo).port, url: "http://example.test/" }, new AbortController().signal, async () => "DIRECT");
    const data = once(stream, "data"); stream.resume(); expect((await data)[0].toString()).toBe("DIRECT"); stream.destroy();
  } finally { server.close(); }
});

it("does not fall back to DIRECT when the system only supplies a failing proxy", async () => {
  const server = http.createServer();
  server.on("connect", (_request, socket) => { socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"); });
  server.listen(0, "127.0.0.1"); await once(server, "listening");
  try {
    await expect(connectPreviewEgress({ host: "example.test", port: 443, url: "https://example.test/" }, new AbortController().signal, async () => `PROXY 127.0.0.1:${(server.address() as net.AddressInfo).port}`)).rejects.toThrow("System HTTP proxy refused");
    expect(fake.lookup).not.toHaveBeenCalled();
  } finally { server.closeAllConnections(); server.close(); }
});
