// Metro bundler configuration for the HarmonyOS app inside the multica
// monorepo. Watches the entire monorepo so type-only imports from
// packages/core/types/* resolve, looks up node_modules from both project and
// monorepo root, and enables symlinks so Metro can follow pnpm's symlinked
// layout to transitive deps. createHarmonyMetroConfig swaps the react-native
// implementation for @react-native-oh/react-native-harmony on the harmony
// platform and redirects @react-native-oh-tpl aliased libraries; NativeWind
// compiles global.css into the runtime style sheet. The "@/..." import alias
// (mirroring apps/mobile's tsconfig paths) resolves ahead of the harmony
// redirect, which stays active for everything else.

const { mergeConfig, getDefaultConfig } = require("@react-native/metro-config");
const {
  createHarmonyMetroConfig,
} = require("@react-native-oh/react-native-harmony/metro.config");
const { withNativeWind } = require("nativewind/metro");
const path = require("path");

const projectRoot = __dirname;
const monorepoRoot = path.resolve(projectRoot, "../..");

const harmonyConfig = createHarmonyMetroConfig({
  reactNativeHarmonyPackageName: "@react-native-oh/react-native-harmony",
});
const harmonyResolveRequest = harmonyConfig.resolver?.resolveRequest;

// The pnpm store holds several react copies (the app pins 19.1.0 while
// peers of @multica/core's dependencies resolve 19.2.x from the catalog),
// and each React instance brings its own AppRegistry/hooks/dispatcher —
// the bundle then dies at launch with "has not been registered" or
// ReactCurrentDispatcher crashes. Force the react family to the app's
// single copy before any other resolution.
const REACT_SINGLETONS = new Set([
  "react",
  "react/jsx-runtime",
  "react-devtools-core",
]);

/**
 * @type {import("metro-config").MetroConfig}
 */
const config = {
  watchFolders: [monorepoRoot],
  resolver: {
    // ONLY the app's own node_modules: the monorepo root hoists the iOS
    // app's react 19.2 / react-native 0.83, and letting Metro fall back to
    // it bakes a second React/AppRegistry into the bundle. Dependencies of
    // watched packages (e.g. @multica/core) resolve through pnpm symlinks
    // from their own locations.
    nodeModulesPaths: [path.resolve(projectRoot, "node_modules")],
    unstable_enableSymlinks: true,
    resolveRequest: (context, moduleName, platform) => {
      if (REACT_SINGLETONS.has(moduleName)) {
        return context.resolveRequest(
          context,
          path.resolve(projectRoot, "node_modules", moduleName),
          platform,
        );
      }
      if (moduleName.startsWith("@/")) {
        return context.resolveRequest(
          context,
          path.resolve(projectRoot, moduleName.slice(2)),
          platform,
        );
      }
      if (harmonyResolveRequest) {
        return harmonyResolveRequest(context, moduleName, platform);
      }
      return context.resolveRequest(context, moduleName, platform);
    },
  },
  transformer: {
    getTransformOptions: async () => ({
      transform: {
        experimentalImportSupport: false,
        // Must stay true for embedded (rawfile) bundles: with eager module
        // evaluation the AppRegistry registration lands after the runtime
        // calls runApplication and the app launches to a white screen
        // ("MulticaHarmony has not been registered"). Verified by bisection:
        // every rawfile bundle built with inlineRequires:false fails to
        // register, including a 20-line minimal entry.
        inlineRequires: true,
      },
    }),
  },
};

module.exports = withNativeWind(
  mergeConfig(getDefaultConfig(projectRoot), harmonyConfig, config),
  { input: "./global.css", inlineRem: 16 },
);
