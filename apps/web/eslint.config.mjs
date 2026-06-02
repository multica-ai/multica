import nextConfig from "@wallts/eslint-config/next";

export default [
  ...nextConfig,
  { ignores: [".next/", ".source/"] },
  {
    files: ["**/*.test.{ts,tsx}", "**/test/**/*.{ts,tsx}"],
    rules: {
      "react/display-name": "off",
    },
  },
];
