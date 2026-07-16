import { createServer } from "node:http";
import { BackendClient } from "./backend-client.mjs";
import { BundleStore } from "./bundle-store.mjs";
import { DeploymentManager } from "./deployment-manager.mjs";
import { SliplaneProvider } from "./providers/sliplane-provider.mjs";
import { createAppsRuntime } from "./runtime.mjs";

const backend = new BackendClient();
const bundleStore = new BundleStore({ backend });
const deploymentManager = process.env.CEREBRO_APPS_RUNTIME_PROVIDER === "sliplane"
  ? new DeploymentManager({ provider: new SliplaneProvider(), backend })
  : undefined;
const runtime = createAppsRuntime({ deploymentManager, backendClient: backend, bundleStore });
const port = Number(process.env.PORT ?? 4310);

deploymentManager?.resume().catch((error) => console.error("mini-app deployment resume failed", { error }));

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
