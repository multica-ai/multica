"use client";

import { usePathname, useRouter } from "next/navigation";
import { DevToolbar, type ScreenshotCapture } from "@multica/views/dev-toolbar";

// A page cannot read its own pixels without asking: the browser's share
// prompt is the price of a faithful screenshot, so it is taken on request
// rather than every time the report opens.
const SCREENSHOT: ScreenshotCapture = {
  capture: captureCurrentTab,
  automatic: false,
};

async function captureCurrentTab(): Promise<Blob> {
  const stream = await navigator.mediaDevices.getDisplayMedia({
    video: { displaySurface: "browser" },
    audio: false,
    preferCurrentTab: true,
  } as DisplayMediaStreamOptions);
  try {
    const video = document.createElement("video");
    video.srcObject = stream;
    video.muted = true;
    await video.play();
    // Give the share prompt time to leave the frame.
    await new Promise((resolve) => setTimeout(resolve, 300));
    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    canvas.getContext("2d")?.drawImage(video, 0, 0);
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error("Empty capture"))), "image/png"),
    );
  } finally {
    stream.getTracks().forEach((track) => track.stop());
  }
}

export function WebDevToolbar({ apiUrl }: { apiUrl: string }) {
  const pathname = usePathname();
  const router = useRouter();
  return (
    <DevToolbar
      build="web"
      apiUrl={apiUrl}
      route={pathname}
      openPath={(path) => router.push(path)}
      screenshot={SCREENSHOT}
    />
  );
}
