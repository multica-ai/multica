/** jsdom has no top-layer dialog API; browser tests cover the real behavior. */
export function mockNativeDialog() {
  const prototype = HTMLDialogElement.prototype;
  const show = Object.getOwnPropertyDescriptor(prototype, "showModal");
  const close = Object.getOwnPropertyDescriptor(prototype, "close");
  const focus = new WeakMap<HTMLDialogElement, Element | null>();
  Object.defineProperties(prototype, {
    showModal: {
      configurable: true,
      value(this: HTMLDialogElement) {
        focus.set(this, document.activeElement);
        this.open = true;
      },
    },
    close: {
      configurable: true,
      value(this: HTMLDialogElement) {
        if (!this.open) return;
        this.open = false;
        const previous = focus.get(this);
        if (previous instanceof HTMLElement) previous.focus();
        this.dispatchEvent(new Event("close"));
      },
    },
  });
  return () => {
    if (show) Object.defineProperty(prototype, "showModal", show);
    else delete (prototype as Partial<HTMLDialogElement>).showModal;
    if (close) Object.defineProperty(prototype, "close", close);
    else delete (prototype as Partial<HTMLDialogElement>).close;
  };
}
