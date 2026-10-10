const { withAppBuildGradle } = require("@expo/config-plugins");

const marker = "// Keep FBJNI aligned with React Native's bundled libc++ runtime.";

module.exports = function withFBJNIAndroidRuntime(config) {
  return withAppBuildGradle(config, (config) => {
    if (config.modResults.language !== "groovy") {
      throw new Error("FBJNI runtime alignment expects a Groovy app/build.gradle");
    }

    const contents = config.modResults.contents;
    if (!contents.includes(marker)) {
      if (!/^dependencies\s*\{/m.test(contents)) {
        throw new Error("Could not find dependencies block in app/build.gradle");
      }

      config.modResults.contents = contents.replace(
        /^dependencies\s*\{/m,
        `${marker}\nconfigurations.configureEach {\n  resolutionStrategy.force("com.facebook.fbjni:fbjni:0.7.0")\n}\n\ndependencies {`,
      );
    }

    return config;
  });
};
