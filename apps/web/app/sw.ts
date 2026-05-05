// PWA service worker compiled by @serwist/next from app/sw.ts → public/sw.js.
//
// This file runs in a ServiceWorker scope (no DOM, no window). It is
// excluded from the main apps/web/tsconfig.json and typechecked via
// tsconfig.sw.json which loads the WebWorker lib instead of DOM. Doing
// it that way (vs. inline `/// <reference lib="webworker"/>`) avoids
// leaking Worker globals into the rest of the app's type universe and
// avoids no-default-lib's project-wide side effects.
import { defaultCache } from "@serwist/next/worker";
import type { PrecacheEntry, SerwistGlobalConfig } from "serwist";
import { Serwist } from "serwist";

declare global {
  interface WorkerGlobalScope extends SerwistGlobalConfig {
    __SW_MANIFEST: (PrecacheEntry | string)[] | undefined;
  }
}

declare const self: ServiceWorkerGlobalScope;

const serwist = new Serwist({
  precacheEntries: self.__SW_MANIFEST,
  skipWaiting: true,
  clientsClaim: true,
  navigationPreload: true,
  runtimeCaching: defaultCache,
});

serwist.addEventListeners();
