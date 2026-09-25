import { flushSync } from "react-dom";
import { createRoot } from "react-dom/client";
import { ShimmerText } from "../components/common/shimmer-text";

const root = createRoot(document.getElementById("mount")!);

// Mount the real component for browser-only layout and accessibility checks.
export function render(text: string, active = true) {
  flushSync(() =>
    root.render(
      <div id="fixture" style={{ width: 180, font: "15px/22px sans-serif" }}>
        <ShimmerText id="label" active={active}>
          {text}
        </ShimmerText>
      </div>,
    ),
  );
}
