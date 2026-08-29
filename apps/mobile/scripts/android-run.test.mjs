import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = fileURLToPath(new URL(".", import.meta.url));
const mobileDir = resolve(scriptDir, "..");

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function assertWrapper(name, expected) {
  const source = readFileSync(resolve(scriptDir, name), "utf8");
  assert(source.includes('"expo", "prebuild", "-p", "android", "--no-install"'), `${name} must prebuild Android`);
  assert(source.includes(expected), `${name} must run ${expected}`);
}

assertWrapper("android-run.mjs", '"expo", "run:android"');
assertWrapper("android-apk.mjs", '"app:assembleDebug"');

function configFor(appEnv) {
  return JSON.parse(
    execFileSync("pnpm", ["exec", "expo", "config", "--type", "public", "--json"], {
      cwd: mobileDir,
      encoding: "utf8",
      env: { ...process.env, APP_ENV: appEnv },
      shell: process.platform === "win32",
    }),
  );
}

function assertConfig(appEnv, expectedPackage) {
  const config = configFor(appEnv);
  assert(config.android?.package === expectedPackage, `expected Android package ${expectedPackage}`);
  assert(config.scheme === "multica", "expected multica scheme");
  assert(config.android?.versionCode === 1, "expected Android versionCode 1");
  assert(config.android?.adaptiveIcon?.foregroundImage === "./assets/icon.png", "expected adaptive icon");
  for (const permission of ["android.permission.INTERNET", "android.permission.READ_MEDIA_IMAGES"]) {
    assert(config.android?.permissions?.includes(permission), `missing ${permission}`);
  }
  assert(
    config.android?.intentFilters?.some(
      (filter) => filter.action === "VIEW" && filter.data?.some((data) => data.scheme === "multica"),
    ),
    "missing multica Android deep-link intent filter",
  );
}

assertConfig("development", "ai.kitta.multica.dev");
assertConfig("staging", "ai.kitta.multica.staging");
assertConfig("production", "ai.kitta.multica");

const eas = JSON.parse(readFileSync(resolve(mobileDir, "eas.json"), "utf8"));
const internal = eas.build?.internal;
assert(internal?.distribution === "internal", "missing internal distribution profile");
assert(internal?.android?.buildType === "apk", "internal Android build must produce an APK");
assert(internal?.env?.APP_ENV === "staging", "internal build must use staging config");
assert(internal?.env?.EXPO_PUBLIC_API_URL === "https://multica-api.copilothub.ai", "internal build must provide staging API URL");
assert(internal?.env?.EXPO_PUBLIC_WEB_URL === "https://multica-app.copilothub.ai", "internal build must provide staging web URL");
