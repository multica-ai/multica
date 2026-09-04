import { app } from "electron";
import { join } from "node:path";

/** The CLI built with this Desktop version; never resolve a downloaded or PATH
 * binary here. In a packaged app, resources are unpacked next to app.asar. */
export function bundledCliPath(): string {
  const binName = process.platform === "win32" ? "multica.exe" : "multica";
  return join(app.getAppPath(), "resources", "bin", binName).replace(
    "app.asar",
    "app.asar.unpacked",
  );
}
