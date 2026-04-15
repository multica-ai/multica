const { getDefaultConfig } = require("expo/metro-config");
const path = require("path");

const monorepoRoot = path.resolve(__dirname, "../..");

const config = getDefaultConfig(__dirname);

// Watch the entire monorepo so Metro sees packages/core/ changes
config.watchFolders = [monorepoRoot];

// Resolve hoisted node_modules from the monorepo root (pnpm)
config.resolver.nodeModulesPaths = [
  path.resolve(__dirname, "node_modules"),
  path.resolve(monorepoRoot, "node_modules"),
];

// Force all workspace packages compiled by Metro to use the React version
// installed for this React Native app. The web app intentionally uses the
// monorepo catalog React version, but RN 0.79's renderer requires React 19.0.0.
config.resolver.extraNodeModules = {
  ...config.resolver.extraNodeModules,
  react: path.resolve(__dirname, "node_modules/react"),
  "react/jsx-runtime": path.resolve(__dirname, "node_modules/react/jsx-runtime.js"),
  "react/jsx-dev-runtime": path.resolve(__dirname, "node_modules/react/jsx-dev-runtime.js"),
  "@tanstack/react-query": path.resolve(
    __dirname,
    "node_modules/@tanstack/react-query",
  ),
  zustand: path.resolve(__dirname, "node_modules/zustand"),
};

// Keep Expo's package export defaults. Adding "import" ahead of "require"
// makes Metro resolve Babel runtime helpers to ESM files, then require them
// as CommonJS, which breaks Hermes with "__interopRequireDefault is not a function".
config.resolver.unstable_enablePackageExports = true;

// Do not let files in packages/* resolve their own workspace node_modules.
// Those package installs target the web/catalog React version, while this
// native app must use the React version paired with react-native-renderer.
config.resolver.disableHierarchicalLookup = true;

// Inject polyfills before any app code runs (fixes Hermes SharedArrayBuffer)
const originalGetPolyfills = config.serializer.getPolyfills;
config.serializer.getPolyfills = (ctx) => [
  path.resolve(__dirname, "src/lib/polyfills.ts"),
  ...(originalGetPolyfills ? originalGetPolyfills(ctx) : []),
];

const originalGetModulesRunBeforeMainModule =
  config.serializer.getModulesRunBeforeMainModule;
config.serializer.getModulesRunBeforeMainModule = () => {
  const preModules = originalGetModulesRunBeforeMainModule
    ? originalGetModulesRunBeforeMainModule()
    : [];
  const firstModule = preModules[0];
  const restModules = preModules.slice(1);

  return [
    ...(firstModule ? [firstModule] : []),
    path.resolve(__dirname, "src/lib/pre-main.ts"),
    ...restModules,
  ];
};

module.exports = config;
