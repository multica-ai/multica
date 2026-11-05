import base from "@multica/eslint-config/base";

export default [
  ...base,
  { ignores: ["dist/**"] },
  {
    files: ["**/*.integration.ts", "vitest.integration.config.ts"],
    rules: {
      "import-x/no-extraneous-dependencies": [
        "error",
        { devDependencies: true },
      ],
    },
  },
];
