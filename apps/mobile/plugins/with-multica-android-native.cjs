const fs = require("node:fs");
const path = require("node:path");
const {
  withAndroidManifest,
  withAppBuildGradle,
  withDangerousMod,
  withMainApplication,
  withProjectBuildGradle,
} = require("@expo/config-plugins");

const PACKAGE_REGISTRATION = "add(FullscreenStatusBarPackage())";
const DEFAULT_GETUI_APP_ID = "zopkAIG3P07bN78Q5CHck8";
const GETUI_MAVEN_REPOSITORY =
  'maven { url "https://mvn.getui.com/nexus/content/repositories/releases/" }';
const GETUI_GTC_DEPENDENCY = "implementation 'com.getui:gtc:3.3.1.0'";
const GETUI_GTSDK_DEPENDENCY = "implementation 'com.getui:gtsdk:3.3.13.0'";
const GETUI_QUERY_ACTION = "com.getui.sdk.action";
const GETUI_INTENT_SERVICE_PERMISSION = "android.permission.BIND_JOB_SERVICE";
const GETUI_DEBUG_LOGGER_IMPORTS = [
  "android.app.ActivityManager",
  "android.app.Application",
  "android.os.Build",
  "android.util.Log",
  "com.igexin.sdk.IUserLoggerInterface",
  "com.igexin.sdk.PushManager",
];
const MAIN_PROCESS_GUARD = "    if (!isMainProcess()) {\n      return\n    }\n";
const CONFIGURATION_MAIN_PROCESS_GUARD =
  "    if (!isMainProcess()) {\n      return\n    }\n";
const GETUI_DEBUG_LOGGER_BLOCK =
  '    if (BuildConfig.DEBUG) {\n' +
  '      PushManager.getInstance().setDebugLogger(this, object : IUserLoggerInterface {\n' +
  '        override fun log(s: String?) {\n' +
  '          Log.i("PUSH_LOG", s ?: "")\n' +
  "        }\n" +
  "      })\n" +
  "    }\n";

function withMulticaAndroidNative(config, options = {}) {
  const getuiAppId = String(options.getuiAppId || DEFAULT_GETUI_APP_ID).trim();

  config = withGetuiMavenRepository(config);
  config = withGetuiAppBuildGradle(config, getuiAppId);
  config = withGetuiAndroidManifest(config);
  config = withAndroidNativeSources(config);
  config = withDebugKeystore(config);
  config = withAndroidPackageRegistrations(config);
  config = withAndroidWindowSettings(config);
  config = withAndroidResourceSettings(config);
  return config;
}

function withGetuiMavenRepository(config) {
  return withProjectBuildGradle(config, (modConfig) => {
    modConfig.modResults.contents = ensureGetuiMavenRepository(
      modConfig.modResults.contents
    );
    return modConfig;
  });
}

function withGetuiAppBuildGradle(config, getuiAppId) {
  return withAppBuildGradle(config, (modConfig) => {
    let contents = modConfig.modResults.contents;
    contents = ensureGetuiManifestPlaceholder(contents, getuiAppId);
    contents = ensureGradleDependency(contents, "com.getui:gtsdk", GETUI_GTSDK_DEPENDENCY);
    contents = ensureGradleDependency(contents, "com.getui:gtc", GETUI_GTC_DEPENDENCY);
    modConfig.modResults.contents = contents;
    return modConfig;
  });
}

function withGetuiAndroidManifest(config) {
  return withAndroidManifest(config, (modConfig) => {
    const androidPackage = getAndroidPackage(modConfig);
    const manifest = modConfig.modResults.manifest;
    const queries = manifest.queries ?? (manifest.queries = [{}]);
    const query = queries[0] ?? (queries[0] = {});
    const intents = query.intent ?? (query.intent = []);
    const hasGetuiQuery = intents.some((intent) =>
      intent.action?.some(
        (action) => action.$?.["android:name"] === GETUI_QUERY_ACTION
      )
    );

    if (!hasGetuiQuery) {
      intents.push({
        action: [
          {
            $: {
              "android:name": GETUI_QUERY_ACTION,
            },
          },
        ],
      });
    }

    const application = manifest.application?.[0];
    if (!application) {
      throw new Error("Unable to find AndroidManifest application node.");
    }
    const serviceName = `${androidPackage}.push.GetuiIntentService`;
    const pushServiceName = `${androidPackage}.push.GetuiPushService`;
    const services = application.service ?? (application.service = []);
    const hasGetuiPushService = services.some(
      (service) => service.$?.["android:name"] === pushServiceName
    );
    if (!hasGetuiPushService) {
      services.push({
        $: {
          "android:name": pushServiceName,
          "android:exported": "false",
          "android:label": "PushService",
          "android:process": ":pushservice",
        },
      });
    }

    const hasGetuiService = services.some(
      (service) => service.$?.["android:name"] === serviceName
    );
    if (!hasGetuiService) {
      services.push({
        $: {
          "android:name": serviceName,
          "android:exported": "false",
          "android:permission": GETUI_INTENT_SERVICE_PERMISSION,
        },
      });
    }

    return modConfig;
  });
}

function withAndroidNativeSources(config) {
  return withDangerousMod(config, [
    "android",
    async (modConfig) => {
      const androidPackage = getAndroidPackage(modConfig);
      const packageDir = androidPackage.replaceAll(".", path.sep);
      const packageOutputDir = path.join(
        modConfig.modRequest.platformProjectRoot,
        "app/src/main/java",
        packageDir
      );
      const pushOutputDir = path.join(packageOutputDir, "push");

      fs.mkdirSync(packageOutputDir, { recursive: true });
      fs.mkdirSync(pushOutputDir, { recursive: true });

      for (const fileName of [
        "FullscreenStatusBarModule.kt",
        "FullscreenStatusBarPackage.kt",
      ]) {
        const templatePath = path.join(__dirname, "android", fileName);
        const outputPath = path.join(packageOutputDir, fileName);
        const contents = fs
          .readFileSync(templatePath, "utf8")
          .replaceAll("__PACKAGE_NAME__", androidPackage);
        writeIfChanged(outputPath, contents);
      }

      for (const fileName of [
        "GetuiPushState.kt",
        "GetuiPushService.kt",
        "GetuiPushModule.kt",
        "GetuiPushPackage.kt",
        "GetuiIntentService.kt",
      ]) {
        const templatePath = path.join(__dirname, "android", fileName);
        const outputPath = path.join(pushOutputDir, fileName);
        const contents = fs
          .readFileSync(templatePath, "utf8")
          .replaceAll("__PACKAGE_NAME__", androidPackage);
        writeIfChanged(outputPath, contents);
      }

      return modConfig;
    },
  ]);
}

function withDebugKeystore(config) {
  return withDangerousMod(config, [
    "android",
    async (modConfig) => {
      const templatePath = path.join(__dirname, "android", "debug.keystore");
      const outputPath = path.join(
        modConfig.modRequest.platformProjectRoot,
        "app/debug.keystore"
      );

      fs.mkdirSync(path.dirname(outputPath), { recursive: true });
      writeBinaryIfChanged(outputPath, fs.readFileSync(templatePath));

      return modConfig;
    },
  ]);
}

function withAndroidPackageRegistrations(config) {
  return withMainApplication(config, (modConfig) => {
    const mainApplication = modConfig.modResults;
    const androidPackage = getAndroidPackage(modConfig);

    if (mainApplication.language !== "kt") {
      throw new Error(
        "with-multica-android-native only supports Kotlin MainApplication files."
      );
    }

    const packageListMarker = /PackageList\(this\)\.packages\.apply\s*\{\n/;
    if (!packageListMarker.test(mainApplication.contents)) {
      throw new Error(
        "Unable to find PackageList(this).packages.apply block in MainApplication.kt."
      );
    }

    const getuiPackageRegistration = `add(${androidPackage}.push.GetuiPushPackage())`;
    for (const registration of [PACKAGE_REGISTRATION, getuiPackageRegistration]) {
      if (mainApplication.contents.includes(registration)) {
        continue;
      }
      mainApplication.contents = mainApplication.contents.replace(
        packageListMarker,
        (match) => `${match}          ${registration}\n`
      );
    }

    mainApplication.contents = ensureKotlinImports(
      mainApplication.contents,
      GETUI_DEBUG_LOGGER_IMPORTS
    );
    mainApplication.contents = ensureMainProcessGuard(mainApplication.contents);
    mainApplication.contents = ensureConfigurationMainProcessGuard(
      mainApplication.contents
    );
    mainApplication.contents = ensureMainProcessHelper(mainApplication.contents);
    mainApplication.contents = ensureGetuiDebugLogger(mainApplication.contents);

    return modConfig;
  });
}

function withAndroidWindowSettings(config) {
  return withAndroidManifest(config, (modConfig) => {
    const application = modConfig.modResults.manifest.application?.[0];
    if (!application) {
      throw new Error("Unable to find AndroidManifest application node.");
    }

    application.$["android:enableOnBackInvokedCallback"] = "false";
    application.$["android:requestLegacyExternalStorage"] = "true";

    return modConfig;
  });
}

function withAndroidResourceSettings(config) {
  return withDangerousMod(config, [
    "android",
    async (modConfig) => {
      const resDir = path.join(modConfig.modRequest.platformProjectRoot, "app/src/main/res");
      const valuesDir = path.join(resDir, "values");

      ensureStyleItem(
        path.join(valuesDir, "styles.xml"),
        "AppTheme",
        '<item name="android:statusBarColor">@android:color/transparent</item>'
      );
      ensureStyleItem(
        path.join(valuesDir, "styles.xml"),
        "AppTheme",
        '<item name="android:navigationBarColor">@android:color/transparent</item>'
      );
      ensureStyle(
        path.join(valuesDir, "styles.xml"),
        '<style name="Theme.App.SplashScreen" parent="AppTheme">\n' +
          '    <item name="android:windowBackground">@drawable/ic_launcher_background</item>\n' +
          "  </style>"
      );
      ensureColor(path.join(valuesDir, "colors.xml"), "colorPrimary", "#023c69");

      return modConfig;
    },
  ]);
}

function getAndroidPackage(config) {
  const androidPackage = config.android?.package;
  if (!androidPackage) {
    throw new Error("android.package is required for with-multica-android-native.");
  }
  return androidPackage;
}

function ensureGetuiMavenRepository(contents) {
  if (contents.includes("https://mvn.getui.com/nexus/content/repositories/releases/")) {
    return contents;
  }

  const jitpackRepository = /maven\s*\{\s*url ['"]https:\/\/www\.jitpack\.io['"]\s*\}/;
  if (!jitpackRepository.test(contents)) {
    throw new Error("Unable to find JitPack repository in android/build.gradle.");
  }

  return contents.replace(
    jitpackRepository,
    (match) => `${match}\n    ${GETUI_MAVEN_REPOSITORY}`
  );
}

function ensureGetuiManifestPlaceholder(contents, getuiAppId) {
  if (!getuiAppId) {
    throw new Error("GETUI_APPID is required for Getui Android integration.");
  }

  const placeholderLine = `            GETUI_APPID: "${escapeGradleString(getuiAppId)}",`;
  if (/GETUI_APPID\s*:\s*["'][^"']*["']\s*,?/.test(contents)) {
    return contents.replace(/^\s*GETUI_APPID\s*:\s*["'][^"']*["']\s*,?\s*$/m, placeholderLine);
  }

  const placeholdersBlock = /(manifestPlaceholders\s*=\s*\[[\s\S]*?)(\n\s*\])/;
  if (placeholdersBlock.test(contents)) {
    return contents.replace(placeholdersBlock, `$1\n${placeholderLine}$2`);
  }

  const versionNameLine = /^(\s*versionName\s+["'][^"']+["']\s*)$/m;
  if (!versionNameLine.test(contents)) {
    throw new Error("Unable to find versionName in android/app/build.gradle.");
  }

  return contents.replace(
    versionNameLine,
    `$1\n\n        manifestPlaceholders = [\n${placeholderLine}\n        ]`
  );
}

function ensureGradleDependency(contents, artifact, dependencyLine) {
  const dependencyPattern = new RegExp(
    `^\\s*implementation ['"]${escapeRegExp(artifact)}:[^'"]+['"].*$`,
    "m"
  );
  if (dependencyPattern.test(contents)) {
    return contents.replace(dependencyPattern, `    ${dependencyLine}`);
  }

  const dependenciesEnd = contents.lastIndexOf("\n}");
  if (dependenciesEnd === -1) {
    throw new Error("Unable to find dependencies block in android/app/build.gradle.");
  }

  return `${contents.slice(0, dependenciesEnd)}\n    ${dependencyLine}${contents.slice(
    dependenciesEnd
  )}`;
}

function ensureKotlinImports(contents, imports) {
  const missingImports = imports.filter(
    (importName) => !contents.includes(`import ${importName}`)
  );
  if (missingImports.length === 0) {
    return contents;
  }

  const packageDeclaration = /^(package [^\n]+\n)/;
  if (!packageDeclaration.test(contents)) {
    throw new Error("Unable to find Kotlin package declaration.");
  }

  return contents.replace(
    packageDeclaration,
    (match) => `${match}\n${missingImports.map((name) => `import ${name}`).join("\n")}\n`
  );
}

function ensureGetuiDebugLogger(contents) {
  if (contents.includes("setDebugLogger(this, object : IUserLoggerInterface")) {
    return contents;
  }

  const onCreateMarker = /(\s*override fun onCreate\(\) \{\n\s*super\.onCreate\(\)\n)/;
  if (!onCreateMarker.test(contents)) {
    throw new Error("Unable to find MainApplication.onCreate super.onCreate marker.");
  }

  return contents.replace(onCreateMarker, (match) => `${match}${GETUI_DEBUG_LOGGER_BLOCK}`);
}

function ensureMainProcessGuard(contents) {
  if (contents.includes("if (!isMainProcess())")) {
    return contents;
  }

  const onCreateMarker = /(\s*override fun onCreate\(\) \{\n\s*super\.onCreate\(\)\n)/;
  if (!onCreateMarker.test(contents)) {
    throw new Error("Unable to find MainApplication.onCreate super.onCreate marker.");
  }

  return contents.replace(onCreateMarker, (match) => `${match}${MAIN_PROCESS_GUARD}`);
}

function ensureMainProcessHelper(contents) {
  if (contents.includes("private fun isMainProcess()")) {
    return contents;
  }

  const helper =
    "  private fun isMainProcess(): Boolean {\n" +
    "    val processName = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {\n" +
    "      Application.getProcessName()\n" +
    "    } else {\n" +
    "      val pid = android.os.Process.myPid()\n" +
    "      val activityManager = getSystemService(ACTIVITY_SERVICE) as? ActivityManager\n" +
    "      activityManager\n" +
    "        ?.runningAppProcesses\n" +
    "        ?.firstOrNull { it.pid == pid }\n" +
    "        ?.processName\n" +
    "    }\n" +
    "    return processName == packageName\n" +
    "  }\n\n";
  const configurationMarker = /\n\s*override fun onConfigurationChanged\(/;
  if (!configurationMarker.test(contents)) {
    throw new Error("Unable to find MainApplication.onConfigurationChanged marker.");
  }

  return contents.replace(configurationMarker, (match) => `\n\n${helper}${match.slice(1)}`);
}

function ensureConfigurationMainProcessGuard(contents) {
  if (
    contents.includes(
      "override fun onConfigurationChanged(newConfig: Configuration) {\n" +
        "    super.onConfigurationChanged(newConfig)\n" +
        "    if (!isMainProcess())"
    )
  ) {
    return contents;
  }

  const configurationMethodMarker =
    /(\s*override fun onConfigurationChanged\(newConfig: Configuration\) \{\n\s*super\.onConfigurationChanged\(newConfig\)\n)/;
  if (!configurationMethodMarker.test(contents)) {
    throw new Error("Unable to find MainApplication.onConfigurationChanged body.");
  }

  return contents.replace(
    configurationMethodMarker,
    (marker) => `${marker}${CONFIGURATION_MAIN_PROCESS_GUARD}`
  );
}

function escapeGradleString(value) {
  return value.replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function ensureStyleItem(filePath, styleName, item) {
  let contents = fs.readFileSync(filePath, "utf8");
  const stylePattern = new RegExp(
    `(<style name="${escapeRegExp(styleName)}"[^>]*>)([\\s\\S]*?)(\\n\\s*</style>)`
  );
  const match = contents.match(stylePattern);
  if (!match) {
    throw new Error(`Unable to find ${styleName} in ${filePath}.`);
  }
  if (match[2].includes(item)) {
    return;
  }
  contents = contents.replace(stylePattern, `$1$2\n    ${item}$3`);
  writeIfChanged(filePath, contents);
}

function ensureStyle(filePath, style) {
  let contents = fs.readFileSync(filePath, "utf8");
  const styleName = style.match(/<style name="([^"]+)"/)?.[1];
  if (!styleName) {
    throw new Error("Style name is required.");
  }
  if (contents.includes(`<style name="${styleName}"`)) {
    return;
  }
  contents = contents.replace(/\n<\/resources>\s*$/, `\n  ${style}\n</resources>\n`);
  writeIfChanged(filePath, contents);
}

function ensureColor(filePath, name, value) {
  let contents = fs.readFileSync(filePath, "utf8");
  const colorPattern = new RegExp(`<color name="${escapeRegExp(name)}">[^<]*</color>`);
  if (colorPattern.test(contents)) {
    contents = contents.replace(colorPattern, `<color name="${name}">${value}</color>`);
  } else {
    contents = contents.replace(
      /\n<\/resources>\s*$/,
      `\n  <color name="${name}">${value}</color>\n</resources>\n`
    );
  }
  writeIfChanged(filePath, contents);
}

function writeIfChanged(filePath, contents) {
  if (fs.existsSync(filePath) && fs.readFileSync(filePath, "utf8") === contents) {
    return;
  }
  fs.writeFileSync(filePath, contents);
}

function writeBinaryIfChanged(filePath, contents) {
  if (fs.existsSync(filePath) && fs.readFileSync(filePath).equals(contents)) {
    return;
  }
  fs.writeFileSync(filePath, contents);
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

module.exports = withMulticaAndroidNative;
