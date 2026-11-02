import reactConfig from "@multica/eslint-config/react";

export default [
  ...reactConfig,
  {
    files: ["icons/drawings/*.mjs"],
    languageOptions: {
      globals: { process: "readonly", console: "readonly", URL: "readonly" },
    },
  },
];
