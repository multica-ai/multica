module.exports = function (api) {
  api.cache(true);
  return {
    presets: [
      // @multica/core's search index spawns its worker via `import.meta.url`,
      // which Hermes cannot parse untransformed. The worker itself never runs
      // here: the index reports itself unavailable without SharedWorker/Worker.
      ["babel-preset-expo", { unstable_transformImportMeta: true }],
    ],
  };
};
