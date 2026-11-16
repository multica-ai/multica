#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const mobileRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
process.chdir(mobileRoot);

function run(command, args, capture = false) {
  const result = spawnSync(command, args, {
    encoding: "utf8",
    stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit",
    env: process.env,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    if (capture && result.stderr) console.error(result.stderr);
    throw new Error(`${command} exited with ${result.status}`);
  }
  return result.stdout?.trim() ?? "";
}

function required(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required; see docs/android-release.md`);
  return value;
}

function endpoint(name, optional = false) {
  const value = optional ? process.env[name] : required(name);
  if (!value) return null;
  const url = new URL(value);
  if (!["https:", "http:"].includes(url.protocol) || url.username || url.password ||
      url.search || url.hash || url.pathname !== "/") {
    throw new Error(`${name} must be an HTTP(S) origin without credentials, path, query or fragment`);
  }
  if (url.protocol === "http:" && process.env.EXPO_ANDROID_ALLOW_CLEARTEXT !== "1") {
    throw new Error(`HTTP requires EXPO_ANDROID_ALLOW_CLEARTEXT=1 (${name})`);
  }
  return url.origin;
}

const env = process.env.APP_ENV ?? "production";
process.env.APP_ENV = env;
// Keep Expo from silently reloading .env.production during a staging build.
process.env.EXPO_NO_DOTENV = "1";
const apiUrl = endpoint("EXPO_PUBLIC_API_URL");
const webUrl = endpoint("EXPO_PUBLIC_WEB_URL", true);
process.env.EXPO_PUBLIC_API_URL = apiUrl;
if (webUrl) process.env.EXPO_PUBLIC_WEB_URL = webUrl;
const keystore = resolve(required("MULTICA_ANDROID_KEYSTORE"));
const passwordFile = resolve(required("MULTICA_ANDROID_STORE_PASSWORD_FILE"));
const keyPasswordFile = resolve(process.env.MULTICA_ANDROID_KEY_PASSWORD_FILE ?? passwordFile);
const alias = required("MULTICA_ANDROID_KEY_ALIAS");
for (const path of [keystore, passwordFile, keyPasswordFile]) {
  if (!existsSync(path)) throw new Error(`Signing file does not exist: ${path}`);
}
const sdk = process.env.ANDROID_HOME ?? required("ANDROID_SDK_ROOT");
const toolsVersion = process.env.ANDROID_BUILD_TOOLS_VERSION ??
  readdirSync(join(sdk, "build-tools")).filter((v) => /^\d+\.\d+\.\d+$/.test(v))
    .sort((a, b) => a.localeCompare(b, "en", { numeric: true })).at(-1);
if (!toolsVersion) throw new Error("Install Android SDK build-tools first");
const tool = (name) => join(sdk, "build-tools", toolsVersion, name);
const architectures = process.env.MULTICA_ANDROID_ABIS ?? "arm64-v8a,x86_64";
if (!architectures.split(",").every((a) => ["arm64-v8a", "armeabi-v7a", "x86", "x86_64"].includes(a))) {
  throw new Error("Invalid MULTICA_ANDROID_ABIS");
}

run("pnpm", ["exec", "expo", "prebuild", "-p", "android", "--no-install"]);
const config = JSON.parse(run("pnpm", ["exec", "expo", "config", "--type", "public", "--json"], true));
const outputDir = join(mobileRoot, "dist", "android", env);
mkdirSync(outputDir, { recursive: true });
run("./android/gradlew", [
  "-p", "android", ":app:assembleRelease", "--no-daemon", "--console=plain",
  `--max-workers=${process.env.MULTICA_ANDROID_BUILD_WORKERS ?? "4"}`,
  `-PreactNativeArchitectures=${architectures}`,
]);
const unsigned = join(mobileRoot, "android/app/build/outputs/apk/release/app-release-unsigned.apk");
if (!existsSync(unsigned)) throw new Error("Unsigned Release APK missing; debug-signed APKs are not accepted");
const signedCheck = spawnSync(tool("apksigner"), ["verify", unsigned], { stdio: "ignore" });
if (signedCheck.status === 0) throw new Error("Expected an unsigned APK before Release signing");
const aligned = join(outputDir, "aligned-unsigned.apk");
run(tool("zipalign"), ["-P", "16", "-f", "4", unsigned, aligned]);
const apk = join(outputDir, `multica-${config.version}-${config.android.versionCode}.apk`);
run(tool("apksigner"), [
  "sign", "--ks", keystore, "--ks-key-alias", alias,
  "--ks-pass", `file:${passwordFile}`, "--key-pass", `file:${keyPasswordFile}`,
  "--out", apk, aligned,
]);
const signature = run(tool("apksigner"), ["verify", "--verbose", "--print-certs", apk], true);
if (signature.includes("CN=Android Debug")) throw new Error("A debug certificate cannot sign this deliverable");
run(tool("zipalign"), ["-c", "-P", "16", "4", apk]);
const entries = run("unzip", ["-Z1", apk], true);
if (!entries.split("\n").includes("assets/index.android.bundle")) {
  throw new Error("Embedded JS bundle missing; APK must run without Metro");
}
const badging = run(tool("aapt"), ["dump", "badging", apk], true);
if (/application-debuggable/.test(badging)) throw new Error("Release APK is debuggable");
const sha256 = createHash("sha256").update(readFileSync(apk)).digest("hex");
const metadata = {
  applicationId: config.android.package,
  version: config.version,
  versionCode: config.android.versionCode,
  minSdk: badging.match(/sdkVersion:'(\d+)'/)?.[1],
  targetSdk: badging.match(/targetSdkVersion:'(\d+)'/)?.[1],
  abis: [...badging.matchAll(/native-code:(.*)/g)].flatMap((m) => [...m[1].matchAll(/'([^']+)'/g)].map((a) => a[1])),
  sourceCommit: run("git", ["rev-parse", "HEAD"], true),
  sourceDirty: run("git", ["status", "--porcelain", "--untracked-files=normal"], true) !== "",
  appEnv: env,
  apiUrl,
  webUrl,
  cleartextAllowed: process.env.EXPO_ANDROID_ALLOW_CLEARTEXT === "1",
  sha256,
  certificateSha256: signature.match(/certificate SHA-256 digest: ([a-fA-F0-9]+)/)?.[1],
  embeddedBundle: true,
  debuggable: false,
  builtAt: new Date().toISOString(),
};
writeFileSync(`${apk}.json`, `${JSON.stringify(metadata, null, 2)}\n`);
writeFileSync(`${apk}.sha256`, `${sha256}  ${apk.split("/").at(-1)}\n`);
writeFileSync(`${apk}.signature.txt`, `${signature}\n`);
console.log(`Release APK: ${apk}\n${JSON.stringify(metadata, null, 2)}`);
