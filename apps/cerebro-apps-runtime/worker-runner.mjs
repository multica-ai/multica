import { pathToFileURL } from "node:url";

const modulePath = process.argv[2];
if (!modulePath) throw new Error("App backend module path is required");

let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const input = JSON.parse(raw || "{}");
const backend = await import(pathToFileURL(modulePath).href);
const handler = backend.default ?? backend.handle;
if (typeof handler !== "function") throw new Error("App backend must export a default handler function");
const output = await handler(input, {
  appId: process.env.MULTICA_APP_ID,
  appVersion: process.env.MULTICA_APP_VERSION,
  tokenEndpoint: process.env.MULTICA_APP_TOKEN_ENDPOINT,
});
process.stdout.write(JSON.stringify(output ?? null));
