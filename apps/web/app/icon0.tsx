import { ImageResponse } from "next/og";

// PWA manifest icon — 192x192 "any" purpose. Matches public/favicon.svg
// at full bleed (rounded square preserved); the Web App Manifest spec
// uses this size for legacy Android home-screen launchers and the
// Microsoft Store install card.
export const size = { width: 192, height: 192 };
export const contentType = "image/png";

export default function Icon192() {
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
          borderRadius: "20%",
        }}
      >
        <svg viewBox="0 0 100 100" width="100%" height="100%">
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
