import "./dom";
import { createDocumentCodec } from "@multica/views/editor/schema/codec";
import { loadConfig } from "./config";
import { createCollabServer, type Logger } from "./server";

const write =
  (level: string) =>
  (msg: string, fields: Record<string, unknown> = {}) =>
    console.log(JSON.stringify({ time: new Date().toISOString(), level, msg, ...fields }));
const log: Logger = { info: write("info"), warn: write("warn"), error: write("error") };

const config = loadConfig();
const { server } = createCollabServer(
  config,
  createDocumentCodec({ cdnDomain: config.cdnDomain }),
  log,
);
await server.listen();
log.info("collab listening", { port: config.port, api: config.apiUrl });
