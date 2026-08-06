"use client";

import { useEffect } from "react";

export function ThemeColorMeta() {
  useEffect(() => {
    const sync = () => {
      const color = getComputedStyle(document.documentElement)
        .getPropertyValue("--background")
        .trim();
      if (!color) return;

      const metas = document.head.querySelectorAll<HTMLMetaElement>("meta[name='theme-color']");
      const meta = metas[0] ?? document.head.appendChild(document.createElement("meta"));

      meta.name = "theme-color";
      meta.removeAttribute("media");
      meta.content = color;
      metas.forEach((duplicate, index) => {
        if (index > 0) duplicate.remove();
      });
    };

    sync();

    const observer = new MutationObserver(sync);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => observer.disconnect();
  }, []);

  return null;
}
