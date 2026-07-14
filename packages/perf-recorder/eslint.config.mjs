import baseConfig from "@multica/eslint-config/base";

// perf-recorder is browser-only plain-TS (no React/JSX), so it uses the base
// config rather than the react config. tsup.config.ts legitimately imports the
// `tsup` build tool (a devDependency); the shared allowlist doesn't cover that
// path, so relax the phantom-dependency rule for it here.
export default [
  ...baseConfig,
  {
    files: ["tsup.config.ts"],
    rules: {
      "import-x/no-extraneous-dependencies": "off",
    },
  },
];
