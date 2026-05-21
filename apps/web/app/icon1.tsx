import { ImageResponse } from "next/og";

// PWA manifest icon — 512x512 "any" purpose. Required for the install
// card on Chromium / Edge and used as a quality source by the OS when
// it needs to upscale beyond 192. Same artwork as icon0.tsx, larger
// raster so the polygon stays crisp on hi-dpi launchers.
export const size = { width: 512, height: 512 };
export const contentType = "image/png";

export default function Icon512() {
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
