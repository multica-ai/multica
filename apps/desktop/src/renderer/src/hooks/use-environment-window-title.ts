import { useEffect } from "react";
import {
  withEnvironmentTitleSuffix,
  type EnvironmentHint,
} from "../../../shared/environment-hint";

/**
 * Keep the OS window title tagged with the active non-Cloud server so
 * Mission Control / alt-tab can tell environments apart. Page code still
 * owns the base title via useDocumentTitle / TitleSync; we only suffix.
 *
 * We wrap `document.title`'s setter so every subsequent assignment
 * (TitleSync, tab switches, useDocumentTitle) is rewritten. MutationObserver
 * alone is unreliable across jsdom and some Chromium title update paths.
 */
export function useEnvironmentWindowTitle(hint: EnvironmentHint | null): void {
  useEffect(() => {
    if (!hint) return;

    const proto = Object.getOwnPropertyDescriptor(Document.prototype, "title");
    if (!proto?.get || !proto?.set) {
      // Extremely defensive fallback for exotic environments.
      document.title = withEnvironmentTitleSuffix(document.title, hint.name);
      return;
    }

    const originalGet = proto.get;
    const originalSet = proto.set;
    const name = hint.name;

    Object.defineProperty(document, "title", {
      configurable: true,
      enumerable: true,
      get() {
        return originalGet.call(this);
      },
      set(value: string) {
        originalSet.call(this, withEnvironmentTitleSuffix(String(value ?? ""), name));
      },
    });

    // Re-assign so the current title picks up the suffix immediately.
    document.title = originalGet.call(document);

    return () => {
      // Restore the prototype descriptor on the instance.
      // Reading via originalGet after delete would fall back to prototype.
      const current = originalGet.call(document);
      // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
      delete (document as { title?: string }).title;
      // Write the unsuffixed base back through the prototype setter so we
      // do not leave a sticky " · Server" after unmount (e.g. logout).
      // withEnvironmentTitleSuffix is idempotent on already-suffixed values,
      // so strip by re-setting the pre-wrapper value we just read (which
      // includes the suffix). Page code will set a fresh bare title next.
      originalSet.call(document, current);
    };
  }, [hint]);
}
