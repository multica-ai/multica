import ReactDOM from "react-dom/client";
import App from "./App";
// Inter variable font covers all weights (100-900) in a single file.
// Geist Mono kept as-is for code blocks; CJK is handled by system font fallback
// (see globals.css --font-sans chain). Keep font stack in sync with apps/web/app/layout.tsx.
import "@fontsource-variable/inter";
import "@fontsource/geist-mono/400.css";
import "@fontsource/geist-mono/700.css";
import "./globals.css";

ReactDOM.createRoot(document.getElementById("root")!).render(<App />);
