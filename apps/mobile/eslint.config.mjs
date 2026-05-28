import reactConfig from "@multica/eslint-config/react";

export default [
  {
    ignores: ["plugins/**/*.cjs"],
  },
  ...reactConfig,
];
