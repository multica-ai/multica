// Metro bundler configuration for the mobile app inside the multica monorepo.
// Watches the entire monorepo so type-only imports from packages/core/types/*
// resolve, looks up node_modules from both project and monorepo root, and
// enables symlinks so Metro can follow pnpm's symlinked layout to transitive
// deps. Hierarchical lookup is left enabled (default) — pnpm needs it.

import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { getDefaultConfig } = require("expo/metro-config");

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = __dirname;
const monorepoRoot = path.resolve(projectRoot, "../..");

const config = getDefaultConfig(projectRoot);

config.watchFolders = [monorepoRoot];
config.resolver.nodeModulesPaths = [
  path.resolve(projectRoot, "node_modules"),
  path.resolve(monorepoRoot, "node_modules"),
];
config.resolver.unstable_enableSymlinks = true;

export default config;
