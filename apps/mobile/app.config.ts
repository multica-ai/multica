import type { ExpoConfig, ConfigContext } from "expo/config";

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
  if (!["development", "staging", "production"].includes(env)) {
    throw new Error(`Unsupported APP_ENV: ${env}`);
  }
  const versionCode = Number(process.env.EXPO_ANDROID_VERSION_CODE ?? "8");
  if (!Number.isSafeInteger(versionCode) || versionCode < 1 || versionCode > 2100000000) {
    throw new Error("EXPO_ANDROID_VERSION_CODE must be an integer from 1 to 2100000000");
  }

  return {
    ...config,
    name: isProd
      ? "Multica"
      : isStaging
        ? "Multica (Staging)"
        : "Multica (Dev)",
    slug: "multica-mobile",
    version: "0.1.5",
    orientation: "portrait",
    userInterfaceStyle: "automatic",
    scheme: "multica",
    // 1024x1024 full-bleed, no-alpha source that belongs to mobile alone. Do
    // not point this back at the desktop icon (apps/desktop/build/icon.png):
    // that one is padded for macOS, and iOS would mask it a second time, which
    // is the white ring from #6995. Expo prebuild generates every required iOS
    // icon size from this single PNG.
    icon: "./assets/icon.png",
    android: {
      package: isProd
        ? (process.env.EXPO_ANDROID_PACKAGE_PROD ?? "ai.multica.mobile")
        : isStaging
          ? (process.env.EXPO_ANDROID_PACKAGE_STAGING ?? "ai.multica.mobile.staging")
          : (process.env.EXPO_ANDROID_PACKAGE_DEV ?? "ai.multica.mobile.dev"),
      versionCode,
      softwareKeyboardLayoutMode: "resize",
      blockedPermissions: [
        "android.permission.SYSTEM_ALERT_WINDOW",
        "android.permission.CAMERA",
        "android.permission.RECORD_AUDIO",
        "android.permission.READ_MEDIA_IMAGES",
        "android.permission.READ_MEDIA_VIDEO",
        "android.permission.READ_EXTERNAL_STORAGE",
        "android.permission.WRITE_EXTERNAL_STORAGE",
      ],
    },
    ios: {
      // Expo keeps the top-level portrait policy for iPhone while adding all
      // iPad orientations required for multitasking when tablet support is on.
      supportsTablet: true,
      // Pins DEVELOPMENT_TEAM on every prebuild. Leaving it unset is the normal
      // path — `expo run:ios` then resolves a signing identity from the Keychain
      // itself, which is right when the Apple ID owns exactly one team. With
      // several (a personal team plus an employer's) it takes the *first*
      // identity found whenever the terminal is non-interactive, writes that
      // choice into the generated ios/, and never clears it again: prebuild only
      // writes DEVELOPMENT_TEAM when a value is present, so a project pinned to
      // the wrong team stays wrong until ios/ is deleted. Setting this re-applies
      // the intended team on every `scripts/ios-run.sh` run, which also repairs
      // an already-mispinned checkout.
      appleTeamId: process.env.EXPO_APPLE_TEAM_ID,
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
          ? (process.env.EXPO_BUNDLE_IDENTIFIER_STAGING ?? "ai.multica.mobile.staging")
          : (process.env.EXPO_BUNDLE_IDENTIFIER_DEV ?? "ai.multica.mobile.dev"),
    },
    plugins: [
      "./plugins/with-android-release",
      "expo-router",
      "expo-image",
      "expo-localization",
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
          android: {
            minSdkVersion: 24,
            usesCleartextTraffic: process.env.EXPO_ANDROID_ALLOW_CLEARTEXT === "1",
          },
          ios: {
            buildReactNativeFromSource: true,
            // SDK 57's supported scene runtime is required for Xcode 27 builds.
            // Keep this in prebuild config so clean native generations retain it.
            enableSceneSupport: true,
          },
        },
      ],
    ],
    extra: { APP_ENV: env },
  };
};
