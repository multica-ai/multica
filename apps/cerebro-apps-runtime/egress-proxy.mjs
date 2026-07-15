import http from "node:http";
import net from "node:net";
import { fileURLToPath } from "node:url";

export function allowedHost(host, allowed) {
  return allowed.has(String(host).toLowerCase());
}

export function createEgressProxy({ allowed = allowedFromEnvironment() } = {}) {
  const server = http.createServer((_request, response) => {
    response.writeHead(405).end();
  });

  server.on("connect", (request, client, head) => {
    const target = parseConnectTarget(request.url);
    if (!target || target.port !== 443 || !allowedHost(target.host, allowed)) {
      client.end("HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n");
      return;
    }

    const upstream = net.connect(target.port, target.host);
    upstream.once("connect", () => {
      client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length > 0) upstream.write(head);
      upstream.pipe(client);
      client.pipe(upstream);
    });
    upstream.once("error", () => {
      if (!client.destroyed) client.end("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n");
    });
    client.once("error", () => upstream.destroy());
  });

  return server;
}

function allowedFromEnvironment() {
  return new Set(
    String(process.env.ALLOWED_HOSTS ?? "")
      .split(",")
      .map((host) => host.trim().toLowerCase())
      .filter(Boolean),
  );
}

function parseConnectTarget(value) {
  try {
    const url = new URL(`https://${value}`);
    if (!url.hostname || url.username || url.password || url.pathname !== "/") return null;
    return { host: url.hostname.toLowerCase(), port: Number(url.port || 443) };
  } catch {
    return null;
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === fileURLToPath(new URL(`file://${process.argv[1]}`))) {
  const port = Number(process.env.PORT || 3128);
  createEgressProxy().listen(port, "0.0.0.0");
}
