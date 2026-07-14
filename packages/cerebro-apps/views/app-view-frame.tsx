"use client";

import { useEffect, useRef } from "react";

export function AppViewFrame({ title, src, onSubmit }: { title: string; src: string; onSubmit?: (value: unknown) => void }) {
  const frame = useRef<HTMLIFrameElement>(null);
  useEffect(() => {
    const allowedOrigin = new URL(src, window.location.href).origin;
    const receive = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow || event.origin !== allowedOrigin) return;
      if (event.data?.type === "multica.app-view.submit") onSubmit?.(event.data.value);
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [src, onSubmit]);
  return <iframe ref={frame} src={src} title={title} sandbox="allow-scripts allow-forms" className="min-h-64 w-full rounded-xl border bg-background" />;
}
