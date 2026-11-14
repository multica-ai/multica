/**
 * The layer a theme's `--backdrop` is painted on: a fixed element at the
 * start of <body>, under everything, styled in appearance.css. While the mode
 * in use has a backdrop, <html> carries `data-backdrop`, which turns the frame
 * and the page to glass.
 *
 * The image is set inline from the computed value, not through `var()`, so a
 * new one can be decoded before it replaces the old: switching themes or
 * saving a theme file never clears the window to repaint it.
 */

export const BACKDROP_ELEMENT_ID = "multica-backdrop";
export const BACKDROP_ATTR = "data-backdrop";

export interface BackdropState {
  /** The background the layer shows, or is about to once its images decode. */
  wanted?: string;
  request: number;
}

const isSet = (value: string) => value !== "" && value !== "none";

/** The url()s in a `background` value. */
export function backdropImages(value: string): string[] {
  return [...value.matchAll(/url\(\s*(["']?)(.*?)\1\s*\)/g)].map((m) => m[2]!).filter(Boolean);
}

function decode(url: string): Promise<void> {
  const image = new Image();
  image.src = url;
  return typeof image.decode === "function" ? image.decode().catch(() => undefined) : Promise.resolve();
}

/**
 * Brings the layer and `data-backdrop` in line with the variables `read`
 * returns, as computed on <html>.
 */
export function syncBackdrop(state: BackdropState, read: (name: string) => string): void {
  const value = read("--backdrop").trim();
  if (!isSet(value)) {
    removeBackdrop(state);
    return;
  }
  document.documentElement.setAttribute(BACKDROP_ATTR, "");

  let layer = document.getElementById(BACKDROP_ELEMENT_ID);
  if (!layer) {
    layer = document.createElement("div");
    layer.id = BACKDROP_ELEMENT_ID;
    layer.setAttribute("aria-hidden", "true");
    layer.appendChild(document.createElement("div"));
    document.body.prepend(layer);
    state.wanted = undefined;
  }
  if (state.wanted === value) return;
  state.wanted = value;
  const request = ++state.request;
  const image = layer.firstElementChild as HTMLElement;
  void Promise.all(backdropImages(value).map(decode)).then(() => {
    if (state.request !== request || !image.isConnected) return;
    image.style.background = value;
  });
}

/** Removes the layer and `data-backdrop`. */
export function removeBackdrop(state: BackdropState): void {
  document.documentElement.removeAttribute(BACKDROP_ATTR);
  document.getElementById(BACKDROP_ELEMENT_ID)?.remove();
  state.wanted = undefined;
  state.request += 1;
}
