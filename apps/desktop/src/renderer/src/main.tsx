import ReactDOM from "react-dom/client";
import App from "./App";
// Inter variable font covers all weights (100-900) in a single file.
// CJK is handled by system font fallback (see globals.css --font-sans chain).
// Keep font stack in sync with apps/web/app/layout.tsx.
// The italic axis ships as a separate file — without it the ~20 semantic italic
// labels and every markdown <em>/blockquote render as synthesized oblique.
import "@fontsource-variable/inter";
import "@fontsource-variable/inter/wght-italic.css";
// Editorial serif — matches web's next/font Source_Serif_4. Loaded app-wide so
// onboarding headings and any future editorial surface can use `font-serif`
// (see tokens.css @theme inline). Variable font = one file covers all weights.
import "@fontsource-variable/source-serif-4";
import "@fontsource-variable/source-serif-4/wght-italic.css";
// Mono must be the variable cut, not discrete 400/700: web gets a variable Geist
// Mono from next/font, so any weight desktop does not load silently snaps to the
// nearest one it has and the same shared component renders at two different
// weights. That was already true for `font-mono font-medium` (500 -> 400) in
// chart.tsx, webhook-event-filter-section.tsx and keyboard-shortcuts-tab.tsx, and
// for inline <code> inheriting 600 from a <strong>, heading or <th> in rendered
// markdown (600 -> 700). One variable file covers 100-900 and closes the whole
// class instead of chasing weights one at a time.
import "@fontsource-variable/geist-mono";
import "./globals.css";

// react-grab: dev-only element inspector. Hold ⌘C (Mac) / Ctrl+C and click any
// element to copy its source path + line + component stack for pasting to an AI.
// Opt-in per developer: only loads when VITE_REACT_GRAB is set in a local,
// gitignored apps/desktop/.env.development.local — it never activates for anyone
// else, and the whole branch is tree-shaken out of production builds. The web app
// wires the same tool via next/script in apps/web/app/layout.tsx.
// See https://www.react-grab.com/
if (import.meta.env.DEV && import.meta.env.VITE_REACT_GRAB) {
  const grab = document.createElement("script");
  grab.src = "//unpkg.com/react-grab/dist/index.global.js";
  grab.crossOrigin = "anonymous";
  document.head.appendChild(grab);
}

ReactDOM.createRoot(document.getElementById("root")!).render(<App />);
