import type { ExpoConfig, ConfigContext } from "expo/config";
import {
  withDangerousMod,
  withInfoPlist,
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

/**
 * Wire LarkSSOSDK (Feishu mobile SSO) into the iOS app at prebuild time.
 *
 * The SDK requires three coordinated pieces that Expo's stock prebuild
 * doesn't know about:
 *
 *   1. A CFBundleURLTypes entry whose CFBundleURLSchemes lists the URL
 *      scheme `cli<appId-without-underscores>` so iOS routes Feishu's
 *      OAuth-completion callback back into Multica via openURL.
 *   2. `lark` in LSApplicationQueriesSchemes so the SDK can probe
 *      `canOpenURL("lark://")` to decide app-jump vs H5 fallback. Without
 *      this, iOS 9+ returns false unconditionally for any unlisted scheme
 *      and the SDK silently picks H5 even on devices with Feishu
 *      installed — which is exactly the "professional app jump" UX this
 *      whole detour exists to deliver.
 *   3. The `LarkSSOSDK` pod from the volcengine spec repo, plus a
 *      LarkSSO.handleURL(url) call inside AppDelegate's
 *      application(open url:options:) so the SDK gets to inspect every
 *      URL the app receives before standard deep-link routing.
 *
 * App ID is read from EXPO_PUBLIC_FEISHU_APP_ID (NOT hardcoded) — it lives
 * in apps/mobile/.env.staging.local, which is gitignored, so the value
 * never lands in version control. The staging scripts load that file via
 * dotenv before invoking expo, so process.env carries it through prebuild.
 * When the var is absent (e.g. a bare `expo prebuild` with no env, or a
 * contributor without the local file) the plugin no-ops with a warning:
 * Feishu login is simply disabled and the rest of the app builds normally.
 * The URL scheme is derived from the app id by stripping the underscore,
 * as the LarkSSOSDK iOS docs specify.
 */
const FEISHU_APP_ID = process.env.EXPO_PUBLIC_FEISHU_APP_ID ?? "";
const FEISHU_URL_SCHEME = FEISHU_APP_ID.replace(/_/g, "");

const withFeishuSSO: ConfigPlugin = (config) => {
  if (!FEISHU_APP_ID) {
    console.warn(
      "[app.config] EXPO_PUBLIC_FEISHU_APP_ID not set — skipping Feishu SSO " +
        "native wiring (Info.plist scheme / Podfile / AppDelegate). Set it in " +
        "apps/mobile/.env.staging.local to enable Feishu login.",
    );
    return config;
  }
  // ── 1. Info.plist: URL scheme + queries scheme ──────────────────────
  config = withInfoPlist(config, (cfg) => {
    const plist = cfg.modResults as Record<string, unknown>;
    const queries = (plist.LSApplicationQueriesSchemes as string[]) ?? [];
    if (!queries.includes("lark")) {
      plist.LSApplicationQueriesSchemes = [...queries, "lark"];
    }
    const urlTypes =
      (plist.CFBundleURLTypes as {
        CFBundleURLName?: string;
        CFBundleURLSchemes?: string[];
      }[]) ?? [];
    const alreadyHas = urlTypes.some((entry) =>
      entry.CFBundleURLSchemes?.includes(FEISHU_URL_SCHEME),
    );
    if (!alreadyHas) {
      plist.CFBundleURLTypes = [
        ...urlTypes,
        {
          CFBundleURLName: "feishu-sso",
          CFBundleURLSchemes: [FEISHU_URL_SCHEME],
        },
      ];
    }
    return cfg;
  });

  // ── 2. Podfile: LarkSSOSDK pod + custom CocoaPods spec source ──────
  config = withDangerousMod(config, [
    "ios",
    async (cfg) => {
      const podfilePath = join(
        cfg.modRequest.platformProjectRoot,
        "Podfile",
      );
      let content = await readFile(podfilePath, "utf8");
      if (content.includes("LarkSSOSDK")) return cfg;
      // The SDK lives on a custom spec repo, not the default CDN. The
      // moment a Podfile declares ANY explicit `source`, CocoaPods stops
      // implicitly including the CDN — so every React Native dependency
      // resolved from the CDN (SocketRocket, etc.) suddenly can't be
      // found. Declare BOTH the CDN and the volcengine repo, CDN first,
      // so existing pods keep resolving and LarkSSOSDK resolves from the
      // custom repo.
      const cdnSource = "source 'https://cdn.cocoapods.org/'";
      const customSource =
        "source 'https://github.com/volcengine/volcengine-specs.git'";
      const sourcesBlock = `${cdnSource}\n${customSource}\n`;
      if (!content.includes(customSource)) {
        content = `${sourcesBlock}${content}`;
      }
      // Inject inside the first `target '<App>' do … end` block — Expo's
      // generated Podfile has exactly one such block for the app target.
      // Append just before the `end` of that block so the pod is scoped
      // to the app target (not test targets or post_install hooks).
      const podLine =
        "  pod 'LarkSSOSDK', '1.2.0', :source => 'https://github.com/volcengine/volcengine-specs.git'";
      const targetMatch = content.match(/(target ['"][^'"]+['"] do[\s\S]*?\n)(end\n)/);
      if (targetMatch && targetMatch.index !== undefined) {
        const insertAt =
          targetMatch.index + targetMatch[1].length;
        content = `${content.slice(0, insertAt)}${podLine}\n${content.slice(insertAt)}`;
      } else {
        // Couldn't find an obvious target block — fail loudly rather than
        // silently producing a Podfile that doesn't include the SDK.
        throw new Error(
          "withFeishuSSO: could not locate `target '<App>' do … end` block in Podfile",
        );
      }
      await writeFile(podfilePath, content);
      return cfg;
    },
  ]);

  // ── 3. AppDelegate.swift: import + handleURL injection ──────────────
  config = withDangerousMod(config, [
    "ios",
    async (cfg) => {
      // The Expo template emits the AppDelegate at
      // ios/<ProjectName>/AppDelegate.swift — but the project name varies
      // per scheme (MulticaStaging vs MulticaDev). Glob it instead of
      // hard-coding the path.
      const iosRoot = cfg.modRequest.platformProjectRoot;
      const { readdir } = await import("node:fs/promises");
      const entries = await readdir(iosRoot, { withFileTypes: true });
      let appDelegatePath: string | null = null;
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        const candidate = join(iosRoot, entry.name, "AppDelegate.swift");
        try {
          await readFile(candidate, "utf8");
          appDelegatePath = candidate;
          break;
        } catch {
          // not here, keep looking
        }
      }
      if (!appDelegatePath) {
        throw new Error(
          "withFeishuSSO: could not find AppDelegate.swift under ios/",
        );
      }
      let content = await readFile(appDelegatePath, "utf8");
      if (content.includes("LarkSSO.handleURL")) return cfg;

      // Insert `import LarkSSOSDK`. The Expo SDK 55 template's first import
      // is `internal import Expo` (note the `internal` access modifier and
      // no trailing module list), so anchor on whichever Expo import form
      // is present and append our import on the next line.
      const importAnchor = /^((?:internal )?import Expo)$/m;
      if (!content.includes("import LarkSSOSDK")) {
        const importedContent = content.replace(
          importAnchor,
          "$1\nimport LarkSSOSDK",
        );
        if (importedContent === content) {
          throw new Error(
            "withFeishuSSO: could not find an `import Expo` line in AppDelegate.swift to anchor the LarkSSOSDK import — Expo template may have changed.",
          );
        }
        content = importedContent;
      }

      // Patch the Linking openURL handler. The Expo SDK 55 template body is:
      //   return super.application(app, open: url, options: options) || RCTLinkingManager.application(app, open: url, options: options)
      // Prepend `LarkSSO.handleURL(url) ||` so the SDK inspects Feishu
      // OAuth-callback URLs FIRST. Swift `||` short-circuits: if the SDK
      // consumes the URL we return true immediately; otherwise the existing
      // super + RCTLinkingManager chain runs unchanged. Anchored on the
      // exact `return super.application(app, open: url` prefix (param name
      // is `app`, not `application`, in this overload).
      const openUrlAnchor = /return (super\.application\(app, open: url, options: options\))/;
      const replaced = content.replace(
        openUrlAnchor,
        "return LarkSSO.handleURL(url) || $1",
      );
      if (replaced === content) {
        throw new Error(
          "withFeishuSSO: could not find the expected `return super.application(app, open: url, options: options)` line in AppDelegate.swift — Expo template may have changed and the patch needs updating.",
        );
      }
      content = replaced;
      await writeFile(appDelegatePath, content);
      return cfg;
    },
  ]);

  return config;
};

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
      withFeishuSSO as unknown as string,
    ],
    extra: { APP_ENV: env },
  };
};
