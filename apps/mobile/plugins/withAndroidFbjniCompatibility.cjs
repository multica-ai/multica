const { withProjectBuildGradle } = require("@expo/config-plugins");

const marker = "// Multica: align fbjni with React Native 0.83.6 and NDK 27.1";
const rule = `
${marker}
// react-native-shiki-engine requests fbjni:+; keep its native binary aligned with RN's fbjni 0.7.0.
allprojects {
  configurations.configureEach {
    resolutionStrategy.force 'com.facebook.fbjni:fbjni:0.7.0'
  }
}
`;

function withAndroidFbjniCompatibility(config) {
  return withProjectBuildGradle(config, (config) => {
    const contents = config.modResults.contents;
    if (!contents.includes(marker)) {
      config.modResults.contents = `${contents.trimEnd()}\n${rule}`;
    }
    return config;
  });
}

module.exports = withAndroidFbjniCompatibility;
