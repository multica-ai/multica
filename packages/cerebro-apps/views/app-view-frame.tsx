"use client";

import { useEffect, useRef } from "react";
import { callAppSdk, type AppSdkRequest } from "../core/api";

type AppSdkMessage = AppSdkRequest & { type: "multica.app-sdk.request"; requestId: string };
const APP_SDK_METHODS = new Set<AppSdkRequest["method"]>([
  "registry.token",
  "storage.get",
  "storage.set",
  "storage.delete",
  "connections.call",
  "workers.invoke",
  "views.submit",
]);

function isAppSdkMethod(value: unknown): value is AppSdkRequest["method"] {
  return typeof value === "string" && APP_SDK_METHODS.has(value as AppSdkRequest["method"]);
}

export function AppViewFrame({ appId, version, title, src, onSubmit }: { appId: string; version: string; title: string; src: string; onSubmit?: (value: unknown) => void }) {
  const frame = useRef<HTMLIFrameElement>(null);
  useEffect(() => {
    const allowedOrigin = new URL(src, window.location.href).origin;
    const appsSegment = src.lastIndexOf("/apps/");
    const runtimeBaseUrl = appsSegment >= 0 ? src.slice(0, appsSegment) : "/api/cerebro/apps-runtime";
    const receive = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow || (event.origin !== allowedOrigin && event.origin !== "null")) return;
      if (event.data?.type === "multica.app-view.submit") onSubmit?.(event.data.value);
      const message = event.data as Partial<AppSdkMessage>;
      if (message.type !== "multica.app-sdk.request" || message.appId !== appId || message.version !== version || typeof message.requestId !== "string" || !Array.isArray(message.args) || !isAppSdkMethod(message.method)) return;
      void callAppSdk({ appId, version, method: message.method, args: message.args }, runtimeBaseUrl)
        .then((value) => frame.current?.contentWindow?.postMessage({ type: "multica.app-sdk.response", requestId: message.requestId, appId, version, ok: true, value }, "*"))
        .catch((cause: unknown) => frame.current?.contentWindow?.postMessage({
          type: "multica.app-sdk.response",
          requestId: message.requestId,
          appId,
          version,
          ok: false,
          error: cause instanceof Error ? cause.message : "App request failed",
        }, "*"));
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [appId, version, src, onSubmit]);
  return <iframe ref={frame} src={src} title={title} sandbox="allow-scripts allow-forms" className="h-full min-h-0 w-full border-0 bg-background" />;
}
