import { contextBridge, ipcRenderer } from "electron";

export const verificationPreload = {
  install() {
    contextBridge.exposeInMainWorld("multicaVerificationDev", {
      onRequest(callback: (request: unknown) => unknown) {
        ipcRenderer.on(
          "verification-dev:request",
          (_event, request: unknown) => {
            const id =
              request && typeof request === "object" && "id" in request
                ? request.id
                : null;
            Promise.resolve()
              .then(() => callback(request))
              .then(
                (value) =>
                  ipcRenderer.send("verification-dev:response", { id, value }),
                () =>
                  ipcRenderer.send("verification-dev:response", {
                    id,
                    unavailable: true,
                  }),
              );
          },
        );
      },
    });
  },
};
