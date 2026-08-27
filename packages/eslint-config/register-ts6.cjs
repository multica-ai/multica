"use strict";

// typescript-eslint 8 still `require("typescript")` and refuses 7.0. The
// workspace `typescript` package is native 7 for `tsc` / Next / Cursor.
// Rewrite that require to the 6.x compiler API before the plugin loads.
require.resolve("@typescript/typescript6/package.json");
const Module = require("module");
const orig = Module._resolveFilename;
Module._resolveFilename = function resolveTypeScript6(request, parent, isMain, options) {
  if (request === "typescript" || request.startsWith("typescript/")) {
    request = request.replace(/^typescript(?=\/|$)/, "@typescript/typescript6");
  }
  return orig.call(this, request, parent, isMain, options);
};
