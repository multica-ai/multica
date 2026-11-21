import { execFileSync } from "node:child_process";
import { join } from "node:path";

export function verifyMacRelease(context, execute = execFileSync) {
  if (context.electronPlatformName !== "darwin") return;
  const policy = context.packager.info.options.publish;
  if (!policy || policy === "never") return;
  // afterSign runs after built-in notarization, before ZIP/DMG creation and
  // publication. Checking in the skill after upload would race CI promotion.
  const app = join(context.appOutDir, `${context.packager.appInfo.productFilename}.app`);
  const options = { stdio: "inherit", timeout: 60_000 };
  execute("spctl", ["--assess", "--type", "execute", "--verbose", app], options);
  execute("xcrun", ["stapler", "validate", app], options);
}

export default verifyMacRelease;
