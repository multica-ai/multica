import globals from "globals";
import reactConfig from "@multica/eslint-config/react";

export default [
  ...reactConfig,
  { ignores: ["out/", "dist/"] },
  {
    files: ["scripts/**/*.{mjs,js}"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
];
