import { withAppBuildGradle, withProjectBuildGradle, type ConfigPlugin } from "expo/config-plugins";

/** Expo's template signs Release with the debug key; sign externally instead. */
const withAndroidRelease: ConfigPlugin = (config) => {
  config = withAppBuildGradle(config, (mod) => {
    const contents = mod.modResults.contents;
    const releaseSigning = /(release \{[\s\S]*?)signingConfig signingConfigs\.debug/;
    if (releaseSigning.test(contents)) {
      mod.modResults.contents = contents.replace(releaseSigning, "$1signingConfig null");
    } else if (!/release \{[\s\S]*?signingConfig null/.test(contents)) {
      throw new Error("Cannot disable the template's Release signing; review android/app/build.gradle");
    }
    return mod;
  });
  return withProjectBuildGradle(config, (mod) => {
    // Shiki's dynamic fbjni dependency otherwise upgrades the C++ ABI beyond
    // React Native's bundled libc++, causing a dlopen failure on cold start.
    const pin = `
// @generated multica-fbjni
allprojects {
  configurations.configureEach {
    resolutionStrategy.force("com.facebook.fbjni:fbjni:0.7.0")
  }
}
// @end multica-fbjni
`;
    mod.modResults.contents = mod.modResults.contents.replace(
      /\n\/\/ @generated multica-fbjni[\s\S]*?\n\/\/ @end multica-fbjni\n/g,
      "",
    ) + pin;
    return mod;
  });
};

export default withAndroidRelease;
