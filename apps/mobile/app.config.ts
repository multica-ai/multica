import type { ExpoConfig, ConfigContext } from "expo/config";
import {
  withDangerousMod,
  withXcodeProject,
  type ConfigPlugin,
} from "@expo/config-plugins";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

/**
 * Disable Xcode 16's User Script Sandboxing.
 *
 * Xcode 16 turned `ENABLE_USER_SCRIPT_SANDBOXING = YES` on by default. That
 * confines build-phase scripts (including React Native / Expo's
 * "Bundle React Native code and images" node invocation) inside a macOS
 * sandbox that denies reads of ios/<app>, ios/Pods, and writes to
 * DerivedData — exactly what those scripts must do, so Archive fails with a
 * wall of "Sandbox: node(...) deny(1) file-read-data" errors.
 *
 * Expo SDK 55's `expo-build-properties` plugin does not expose this knob
 * yet, so we apply it via `withXcodeProject` at prebuild time. Flipping it
 * by hand in `project.pbxproj` would be reverted on the next `expo
 * prebuild` (or `pnpm ios:mobile*`, which runs prebuild). Setting it on
 * every XCBuildConfiguration (app target + Pods targets) is intentional and
 * safe — the flag only matters where run-script phases exist.
 */
/**
 * Make Xcode GUI Archive + bare `xcodebuild archive` (CI) bundle JS with the
 * correct EXPO_PUBLIC_* env vars, without depending on the caller's shell.
 *
 * Background: the Bundle React Native code build phase sources
 * `ios/.xcode.env` before invoking Metro + babel-preset-expo, which inlines
 * `process.env.EXPO_PUBLIC_*` at compile time. For `pnpm ios:mobile:staging`
 * the env is injected via `dotenv-cli` in package.json; but Xcode GUI Archive
 * and `xcodebuild archive` skip that wrapper and ship the bundle with empty
 * EXPO_PUBLIC_API_URL — which is exactly the bug that surfaced in the first
 * 0.1.0 Enterprise IPA (verify-code requests went to "undefined/auth/...").
 *
 * Fix: append an env-loading block to `ios/.xcode.env` so the build phase
 * sources the right `.env.<APP_ENV>` file before Metro runs. APP_ENV falls
 * back to a value inferred from `PRODUCT_BUNDLE_IDENTIFIER` so the GUI
 * Archive of MulticaStaging always loads `.env.staging` with zero manual
 * setup. `set -a` ensures variables propagate as exports to the spawned
 * node process (plain `source` would only set them shell-local).
 *
 * `ios/.xcode.env` is checked into git, but `expo prebuild` regenerates it
 * to a minimal default (just NODE_BINARY). `withDangerousMod` runs after the
 * prebuild regeneration on every iOS build, re-appending the block — keyed
 * off the sentinel comment so re-runs stay idempotent.
 */
const XCODE_ENV_VARIANT_BLOCK = `
# === Multica: variant-aware EXPO_PUBLIC_* env loading ===
# See withVariantEnvInXcodeEnv in app.config.ts for why this block exists.
# Edit there, not here — \`expo prebuild\` rewrites this file.
if [ -z "$APP_ENV" ]; then
  case "$PRODUCT_BUNDLE_IDENTIFIER" in
    *.staging) export APP_ENV=staging ;;
    *.dev)     export APP_ENV=development ;;
    *)         export APP_ENV=production ;;
  esac
fi
ENV_FILE="$PROJECT_DIR/../.env.$APP_ENV"
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi
`;

const withVariantEnvInXcodeEnv: ConfigPlugin = (config) =>
  withDangerousMod(config, [
    "ios",
    async (cfg) => {
      const file = join(cfg.modRequest.platformProjectRoot, ".xcode.env");
      let content = "";
      try {
        content = await readFile(file, "utf8");
      } catch {
        // File didn't exist yet — expo prebuild will have created it just
        // before this mod runs, but defend against ordering changes.
      }
      const sentinel = "Multica: variant-aware EXPO_PUBLIC_*";
      if (!content.includes(sentinel)) {
        await writeFile(file, content + XCODE_ENV_VARIANT_BLOCK);
      }
      return cfg;
    },
  ]);

const withDisableUserScriptSandboxing: ConfigPlugin = (config) =>
  withXcodeProject(config, (cfg) => {
    const xcodeProject = cfg.modResults;
    const configurations = xcodeProject.pbxXCBuildConfigurationSection();
    for (const key of Object.keys(configurations)) {
      const entry = configurations[key];
      // Skip COMMENT entries — pbx.js represents each section as
      // { uuid: object, uuid_comment: "Debug" } pairs.
      if (typeof entry !== "object" || !entry?.buildSettings) continue;
      entry.buildSettings.ENABLE_USER_SCRIPT_SANDBOXING = "NO";
    }
    return cfg;
  });

/**
 * Dynamic Expo config — replaces app.json so we can read APP_ENV at runtime
 * and switch bundleIdentifier / display name for dev / staging / production.
 *
 * APP_ENV is set by package.json scripts:
 *   - dev          → APP_ENV unset (treated as "development")
 *   - dev:staging  → APP_ENV=staging
 *   - dev:prod     → APP_ENV=production (rare; usually only for EAS build)
 */
export default ({ config }: ConfigContext): ExpoConfig => {
  const env = process.env.APP_ENV ?? "development";
  const isProd = env === "production";
  const isStaging = env === "staging";

  return {
    ...config,
    name: isProd
      ? "Multica"
      : isStaging
        ? "Multica (Staging)"
        : "Multica (Dev)",
    slug: "multica-mobile",
    version: "0.1.0",
    orientation: "portrait",
    userInterfaceStyle: "automatic",
    scheme: "multica",
    // 1024x1024 source shared with the desktop client
    // (apps/desktop/build/icon.png). Expo prebuild generates every required
    // iOS icon size from this single PNG.
    icon: "./assets/icon.png",
    ios: {
      supportsTablet: false,
      // Per-variant bundle id overrides exist for one reason: an Apple ID
      // can only sign bundle prefixes it owns, so contributors not on the
      // Multica Apple Developer team (and external users self-building a
      // personal copy against production) need to swap to a reverse-domain
      // they control. Each variant has its own `_<VARIANT>` suffix and is
      // only read inside that variant's branch — a generic
      // `EXPO_BUNDLE_IDENTIFIER` would leak across variants (Expo CLI
      // auto-loads `.env.<mode>.local` regardless of APP_ENV) and collapse
      // dev / staging / prod onto a single id.
      //
      // Staging is hardcoded to `lilithgames.*` rather than upstream's
      // `ai.multica.*` because this fork ships staging as an In-House
      // Enterprise build under Lilith's Apple Developer team (8DCHGYMM27),
      // and Apple won't let one team sign a prefix registered to a
      // different team. Keeping it hardcoded means a fresh `expo prebuild`
      // produces a buildable Xcode project on a Lilith member's machine
      // without extra env config — the bundle id has to match what the
      // Lilith Enterprise provisioning profile authorizes.
      bundleIdentifier: isProd
        ? (process.env.EXPO_BUNDLE_IDENTIFIER_PROD ?? "ai.multica.mobile")
        : isStaging
          ? "lilithgames.multica.mobile.staging"
          : (process.env.EXPO_BUNDLE_IDENTIFIER_DEV ?? "ai.multica.mobile.dev"),
    },
    plugins: [
      "expo-router",
      "expo-secure-store",
      "@react-native-community/datetimepicker",
      "react-native-enriched-markdown",
      [
        "expo-image-picker",
        {
          // iOS NSPhotoLibraryUsageDescription. Without this string in
          // Info.plist, calling launchImageLibraryAsync hard-crashes on
          // iOS 14+. Camera + microphone are disabled — we only ever read
          // from the existing photo library.
          photosPermission:
            "Allow Multica to access your photos to attach images to issues and comments.",
          cameraPermission: false,
          microphonePermission: false,
        },
      ],
      [
        "expo-build-properties",
        {
          ios: {
            buildReactNativeFromSource: true,
          },
        },
      ],
      // Expo's strict `PluginEntry` type only includes string / tuple forms,
      // but the runtime resolver accepts a ConfigPlugin function directly.
      // Cast through `unknown` to apply the inline plugin without writing a
      // separate file just to satisfy the type.
      withDisableUserScriptSandboxing as unknown as string,
      withVariantEnvInXcodeEnv as unknown as string,
    ],
    extra: { APP_ENV: env },
  };
};
