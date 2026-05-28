const fs = require("node:fs");
const path = require("node:path");
const {
  withAndroidManifest,
  withDangerousMod,
  withMainApplication,
} = require("@expo/config-plugins");

const PACKAGE_REGISTRATION = "add(FullscreenStatusBarPackage())";

function withMulticaAndroidNative(config) {
  config = withFullscreenStatusBarSources(config);
  config = withDebugKeystore(config);
  config = withFullscreenStatusBarRegistration(config);
  config = withAndroidWindowSettings(config);
  config = withAndroidResourceSettings(config);
  return config;
}

function withFullscreenStatusBarSources(config) {
  return withDangerousMod(config, [
    "android",
    async (modConfig) => {
      const androidPackage = getAndroidPackage(modConfig);
      const packageDir = androidPackage.replaceAll(".", path.sep);
      const outputDir = path.join(
        modConfig.modRequest.platformProjectRoot,
        "app/src/main/java",
        packageDir
      );

      fs.mkdirSync(outputDir, { recursive: true });

      for (const fileName of [
        "FullscreenStatusBarModule.kt",
        "FullscreenStatusBarPackage.kt",
      ]) {
        const templatePath = path.join(__dirname, "android", fileName);
        const outputPath = path.join(outputDir, fileName);
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

function withFullscreenStatusBarRegistration(config) {
  return withMainApplication(config, (modConfig) => {
    const mainApplication = modConfig.modResults;

    if (mainApplication.language !== "kt") {
      throw new Error(
        "with-multica-android-native only supports Kotlin MainApplication files."
      );
    }

    if (mainApplication.contents.includes(PACKAGE_REGISTRATION)) {
      return modConfig;
    }

    const packageListMarker = /PackageList\(this\)\.packages\.apply\s*\{\n/;
    if (!packageListMarker.test(mainApplication.contents)) {
      throw new Error(
        "Unable to find PackageList(this).packages.apply block in MainApplication.kt."
      );
    }

    mainApplication.contents = mainApplication.contents.replace(
      packageListMarker,
      (match) => `${match}          ${PACKAGE_REGISTRATION}\n`
    );

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
