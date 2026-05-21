import { ImageResponse } from "next/og";

// PWA manifest icon — 512x512 "maskable" purpose. Android adaptive icons
// crop the icon down to the launcher's mask shape, so the artwork must
// fill the entire canvas (no rounded corners) and keep the logo inside
// the inner ~80% safe zone defined by the W3C maskable spec. We render
// the polygon at 60% width centered on a white field — matches the safe
// zone with comfortable margin.
export const size = { width: 512, height: 512 };
export const contentType = "image/png";

export default function IconMaskable512() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "#ffffff",
        }}
      >
        <svg viewBox="0 0 100 100" width="60%" height="60%">
          <polygon
            fill="#111827"
            points="45,62.1 45,100 55,100 55,62.1 81.8,88.9 88.9,81.8 62.1,55 100,55 100,45 62.1,45 88.9,18.2 81.8,11.1 55,37.9 55,0 45,0 45,37.9 18.2,11.1 11.1,18.2 37.9,45 0,45 0,55 37.9,55 11.1,81.8 18.2,88.9"
          />
        </svg>
      </div>
    ),
    { ...size },
  );
}
