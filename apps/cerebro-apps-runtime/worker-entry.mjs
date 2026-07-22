import { createServer } from "node:http";

import { createWorkerRuntime } from "./worker-server.mjs";
import { createHostClient } from "./host-client.mjs";
import { workerCommitFromEnvironment } from "./providers/sliplane-provider.mjs";

const runtime = await createWorkerRuntime({
  appId: process.env.APP_ID,
  version: process.env.APP_VERSION,
  bundleUrl: process.env.BUNDLE_URL,
  bundleToken: process.env.BUNDLE_TOKEN,
  expectedBundleSha256: process.env.BUNDLE_SHA256,
  workerCommit: workerCommitFromEnvironment(),
  invokeKey: process.env.INVOKE_KEY,
  hostFactory: (grantToken) => createHostClient({ baseUrl: process.env.BACKEND_URL, grantToken }),
});

const port = Number(process.env.PORT ?? 4311);
createServer(async (req, res) => {
  const chunks = [];
  let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > 512 << 10) {
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
}).listen(port, "0.0.0.0", () => console.log(`Cerebro app worker listening on :${port}`));
