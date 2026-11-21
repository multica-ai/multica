/* global require */
// eslint-disable-next-line @typescript-eslint/no-require-imports -- Sandboxed Electron preloads use CommonJS.
const { contextBridge, ipcRenderer } = require("electron");

// Copied as a standalone preload so the sandbox never needs to require a chunk.
/** @type {import("../shared/run-island").RunIslandAPI} */
const api = {
  subscribe(listener) {
    const receive = (_event, view) => listener(view);
    ipcRenderer.on("run-island:view", receive);
    ipcRenderer.send("run-island:ready");
    return () => {
      ipcRenderer.removeListener("run-island:view", receive);
    };
  },
  expand: (expanded) => ipcRenderer.send("run-island:expand", expanded),
  open: (id) => ipcRenderer.send("run-island:open", id),
};
contextBridge.exposeInMainWorld("runIsland", api);
