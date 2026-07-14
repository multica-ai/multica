import { createServer } from "node:http";
import { createAppsRuntime } from "./runtime.mjs";

const runtime = createAppsRuntime();
const port = Number(process.env.PORT ?? 4310);

createServer(async (req, res) => {
  const chunks = [];
  let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > 1_048_576) {
      res.writeHead(413).end();
      return;
    }
    chunks.push(chunk);
  }
  const request = new Request(`http://${req.headers.host ?? "localhost"}${req.url ?? "/"}`, {
    method: req.method,
    headers: req.headers,
    body: chunks.length ? Buffer.concat(chunks) : undefined,
  });
  const response = await runtime.fetch(request);
  res.writeHead(response.status, Object.fromEntries(response.headers.entries()));
  res.end(Buffer.from(await response.arrayBuffer()));
}).listen(port, "0.0.0.0", () => console.log(`Cerebro apps runtime listening on :${port}`));
