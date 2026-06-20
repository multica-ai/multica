import "@testing-library/jest-dom/vitest";

// jsdom has no matchMedia; useIsMobile (used by DocumentViewPage) calls it on
// mount, so provide a minimal stub that reports "not mobile".
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList;
}
