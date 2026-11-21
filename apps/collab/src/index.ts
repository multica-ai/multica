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

/** Long enough to store every open doc; a stuck store must not block a deploy. */
const SHUTDOWN_TIMEOUT_MS = 15_000;
let stopping = false;
for (const signal of ["SIGTERM", "SIGINT"] as const) {
  process.on(signal, () => {
    if (stopping) return;
    stopping = true;
    log.info("collab stopping", { signal, documents: server.hocuspocus.getDocumentsCount() });
    setTimeout(() => {
      log.warn("collab stop timed out", { documents: server.hocuspocus.getDocumentsCount() });
      process.exit(1);
    }, SHUTDOWN_TIMEOUT_MS).unref();
    // Closes every connection and stores what editors typed before exiting.
    server.destroy().then(
      () => process.exit(0),
      (error: unknown) => {
        log.error("collab stop failed", { error: String(error) });
        process.exit(1);
      },
    );
  });
}
