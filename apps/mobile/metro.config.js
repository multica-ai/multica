// Expo's default config already watches the pnpm workspace root and resolves
// @multica/core's TypeScript sources through its package `exports`.
const { getDefaultConfig } = require("expo/metro-config");

module.exports = getDefaultConfig(__dirname);
