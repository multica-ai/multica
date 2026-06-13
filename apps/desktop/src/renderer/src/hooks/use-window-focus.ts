import { useEffect, useState } from "react";

export function readWindowFocus(): boolean {
  if (typeof document.hasFocus !== "function") return true;
  return document.hasFocus();
}

export function useWindowFocus(): boolean {
  const [isFocused, setIsFocused] = useState(readWindowFocus);

  useEffect(() => {
    const syncFocus = () => {
      setIsFocused(readWindowFocus());
    };

    window.addEventListener("focus", syncFocus);
    window.addEventListener("blur", syncFocus);
    document.addEventListener("visibilitychange", syncFocus);
    syncFocus();

    return () => {
      window.removeEventListener("focus", syncFocus);
      window.removeEventListener("blur", syncFocus);
      document.removeEventListener("visibilitychange", syncFocus);
    };
  }, []);

  return isFocused;
}
