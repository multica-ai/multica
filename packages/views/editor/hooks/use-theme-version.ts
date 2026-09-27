import { useEffect, useState } from "react";

/**
 * A counter that bumps whenever the app theme may have changed: a class,
 * style or data-theme change on <html>/<body>, or the OS color scheme.
 *
 * Sandboxed blocks copy resolved theme colors into their iframe document, so
 * they cannot follow a theme switch through CSS; they rebuild when this bumps.
 */
export function useThemeVersion(): number {
  const [themeVersion, setThemeVersion] = useState(0);

  useEffect(() => {
    const bumpThemeVersion = () => setThemeVersion((version) => version + 1);
    const observer = new MutationObserver(bumpThemeVersion);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    if (document.body) {
      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ["class", "style", "data-theme"],
      });
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", bumpThemeVersion);

    return () => {
      observer.disconnect();
      mediaQuery.removeEventListener("change", bumpThemeVersion);
    };
  }, []);

  return themeVersion;
}
