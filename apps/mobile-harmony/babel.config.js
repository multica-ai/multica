module.exports = {
  presets: ["module:@react-native/babel-preset", "nativewind/babel"],
  plugins: [
    // zod ships ESM "export *" which the RN 0.82 preset's transform order
    // rejects in bundle (jest-worker) mode.
    "@babel/plugin-transform-export-namespace-from",
    [
      "babel-plugin-transform-inline-environment-variables",
      {
        include: ["MULTICA_API_URL", "MULTICA_WEB_URL"],
      },
    ],
  ],
};
