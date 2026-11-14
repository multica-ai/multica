const { defineConfig } = require("eslint/config");
const expoConfig = require("eslint-config-expo/flat");

// Hermes has none of these; the call throws on the device while the browser preview and Node tests pass.
const MISSING_ON_HERMES = ["toSorted", "toReversed", "toSpliced"].map((property) => ({
  property,
  message: `Hermes has no Array.prototype.${property}. Copy the array and use the mutating method instead.`,
}));

module.exports = defineConfig([
  expoConfig,
  {
    ignores: ["dist/*", "ios/*", "android/*", ".expo/*"],
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    rules: { "no-restricted-properties": ["error", ...MISSING_ON_HERMES] },
  },
]);
