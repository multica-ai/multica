import reactConfig from "@multica/eslint-config/react";
import { NO_ROUND_CONTROLS } from "@multica/eslint-config/control-shape";

export default [
  { ignores: ["storybook-static/**"] },
  ...reactConfig,
  {
    files: ["**/*.tsx"],
    rules: {
      "no-restricted-syntax": ["error", NO_ROUND_CONTROLS],
    },
  },
];
