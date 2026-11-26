import { registerProjectFoldersExtension, registerSecretScanningExtension } from "@multica/views/extensions";
import { projectFoldersExtension } from "./project-folders/components";
import { secretScanningExtension } from "./secret-scanning/components";

/**
 * Turns on the commercial features (packages/ee) in an app. Call once at
 * startup, before the first render. Files in this package are covered by
 * ./LICENSE, not the Multica License.
 */
export function registerEE(): void {
  registerProjectFoldersExtension(projectFoldersExtension);
  registerSecretScanningExtension(secretScanningExtension);
}
