import { ImageResponse } from "next/og";

// iOS apple-touch-icon — 180x180 is the modern Retina baseline; iOS
// automatically masks the corners for the home-screen badge so we render
// a flat square (no rounded rect baked in). Logo geometry mirrors
// public/favicon.svg.
export const size = { width: 180, height: 180 };
export const contentType = "image/png";

export default function AppleIcon() {
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
        <svg viewBox="0 0 100 100" width="78%" height="78%">
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
