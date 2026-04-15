/**
 * Expo config plugin: fixes fmt library consteval errors on Xcode 26+.
 *
 * The fmt version bundled in React Native unconditionally detects consteval
 * support without an #ifndef guard, so -D flags can't override it.
 * This plugin adds a post_install hook to the Podfile that:
 * 1. Patches fmt/base.h to wrap the detection in #ifndef FMT_USE_CONSTEVAL
 * 2. Sets FMT_USE_CONSTEVAL=0 globally via GCC_PREPROCESSOR_DEFINITIONS
 */
const { withDangerousMod } = require("expo/config-plugins");
const fs = require("fs");
const path = require("path");

const PATCH_MARKER = "FMT_USE_CONSTEVAL guard";

const PODFILE_SNIPPET = `
    # [withFmtFix] Patch fmt for Xcode 26+ consteval compatibility
    fmt_base = File.join(__dir__, 'Pods', 'fmt', 'include', 'fmt', 'base.h')
    if File.exist?(fmt_base)
      content = File.read(fmt_base)
      unless content.include?('${PATCH_MARKER}')
        content.sub!(
          "// Detect consteval, C++20 constexpr extensions and std::is_constant_evaluated.\\n#if !defined(__cpp_lib_is_constant_evaluated)",
          "// Detect consteval, C++20 constexpr extensions and std::is_constant_evaluated.\\n#ifndef FMT_USE_CONSTEVAL\\n#if !defined(__cpp_lib_is_constant_evaluated)"
        )
        content.sub!(
          "#  define FMT_USE_CONSTEVAL 0\\n#endif\\n#if FMT_USE_CONSTEVAL",
          "#  define FMT_USE_CONSTEVAL 0\\n#endif\\n#endif // ${PATCH_MARKER}\\n#if FMT_USE_CONSTEVAL"
        )
        File.write(fmt_base, content)
      end
    end

    installer.pods_project.targets.each do |target|
      target.build_configurations.each do |build_config|
        defs = build_config.build_settings['GCC_PREPROCESSOR_DEFINITIONS'] ||= ['$(inherited)']
        defs << 'FMT_USE_CONSTEVAL=0' unless defs.include?('FMT_USE_CONSTEVAL=0')
      end
    end
`;

function withFmtFix(config) {
  return withDangerousMod(config, [
    "ios",
    async (config) => {
      const podfilePath = path.join(
        config.modRequest.platformProjectRoot,
        "Podfile"
      );

      if (!fs.existsSync(podfilePath)) return config;

      let podfile = fs.readFileSync(podfilePath, "utf-8");

      if (!podfile.includes(PATCH_MARKER)) {
        // Insert after "post_install do |installer|"
        podfile = podfile.replace(
          /(post_install do \|installer\|)/,
          `$1\n${PODFILE_SNIPPET}`
        );
        fs.writeFileSync(podfilePath, podfile);
      }

      return config;
    },
  ]);
}

module.exports = withFmtFix;
