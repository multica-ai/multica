import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { ExpoConfig, ConfigContext } from "expo/config";
import { withDangerousMod } from "expo/config-plugins";
import type { ConfigPlugin } from "expo/config-plugins";

const ANDROID_ADAPTIVE_ICON_BACKGROUND = "#ffffff";
const ANDROID_GRADLE_VERSION = "8.13";
type AndroidConfig = NonNullable<ExpoConfig["android"]> & {
  edgeToEdgeEnabled?: boolean;
};

const withAndroidGradleWrapper: ConfigPlugin = (config) =>
  withDangerousMod(config, [
    "android",
    async (modConfig) => {
      const wrapperPath = join(
        modConfig.modRequest.platformProjectRoot,
        "gradle",
        "wrapper",
        "gradle-wrapper.properties",
      );
      const contents = await readFile(wrapperPath, "utf8");
      const nextContents = contents.replace(
        /distributionUrl=.*gradle-[^/]+-(bin|all)\.zip/,
        `distributionUrl=https\\://services.gradle.org/distributions/gradle-${ANDROID_GRADLE_VERSION}-bin.zip`,
      );

      if (
        nextContents === contents &&
        !contents.includes(`gradle-${ANDROID_GRADLE_VERSION}-bin.zip`)
      ) {
        throw new Error(`Expected Gradle wrapper distributionUrl in ${wrapperPath}`);
      }

      if (nextContents !== contents) {
        await writeFile(wrapperPath, nextContents);
      }

      return modConfig;
    },
  ]);

/**
 * Dynamic Expo config — replaces app.json so we can read APP_ENV at runtime
 * and switch bundleIdentifier / package / display name for dev / staging /
 * production.
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

  const androidConfig: AndroidConfig = {
    package: isProd
      ? (process.env.EXPO_ANDROID_PACKAGE_PROD ?? "ai.multica.mobile")
      : isStaging
        ? (process.env.EXPO_ANDROID_PACKAGE_STAGING ?? "ai.multica.mobile.staging")
        : (process.env.EXPO_ANDROID_PACKAGE_DEV ?? "ai.multica.mobile.dev"),
    adaptiveIcon: {
      foregroundImage: "./assets/icon.png",
      backgroundColor: ANDROID_ADAPTIVE_ICON_BACKGROUND,
    },
    edgeToEdgeEnabled: true,
  };

  const expoConfig: ExpoConfig = {
    ...config,
    name: isProd ? "Multica" : isStaging ? "Multica (Staging)" : "Multica (Dev)",
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
      bundleIdentifier: isProd
        ? (process.env.EXPO_BUNDLE_IDENTIFIER_PROD ?? "ai.multica.mobile")
        : isStaging
          ? "ai.multica.mobile.staging"
          : (process.env.EXPO_BUNDLE_IDENTIFIER_DEV ?? "ai.multica.mobile.dev"),
    },
    android: androidConfig,
    plugins: [
      "expo-router",
      "expo-secure-store",
      "@react-native-community/datetimepicker",
      "react-native-enriched-markdown",
      [
        "expo-image-picker",
        {
          // Expo only exposes iOS permission copy here. Android uses the
          // platform picker / permission UI, while `microphonePermission: false`
          // still suppresses RECORD_AUDIO there.
          photosPermission:
            "Allow Multica to access your photos to attach images to issues and comments.",
          cameraPermission: false,
          microphonePermission: false,
        },
      ],
      [
        "expo-build-properties",
        {
          android: {},
          ios: {
            buildReactNativeFromSource: true,
          },
        },
      ],
    ],
    extra: { APP_ENV: env },
  };

  return withAndroidGradleWrapper(expoConfig);
};
