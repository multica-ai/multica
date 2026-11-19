"use client";

// Side-effect import: pulls the i18next module augmentation into the
// compilation graph. Without this, apps that consume @multica/views won't
// see the resources types or the selector-API enablement, and their
// typecheck would reject `t($ => $.foo.bar)` calls inside views.
import "./resources-types";

import { useContext, useMemo, useSyncExternalStore } from "react";
import type { i18n as I18n } from "i18next";
import { I18nContext, getI18n, type useTranslation } from "react-i18next";

// The public type is react-i18next's (see the export below); internally a
// namespace is a runtime string.
type AnyT = (...args: any[]) => any;

/**
 * Language changes per i18next instance, shared by every component: one
 * listener per instance instead of one per mounted component.
 */
interface LanguageStore {
  revision: number;
  listeners: Set<() => void>;
  subscribe: (listener: () => void) => () => void;
  /** Changes identity with the language, as react-i18next's `i18n` does. */
  view: I18n;
  fixedT: Map<string, AnyT>;
}

const stores = new WeakMap<I18n, LanguageStore>();

function storeFor(i18n: I18n): LanguageStore {
  const known = stores.get(i18n);
  if (known) return known;
  const store: LanguageStore = {
    revision: 0,
    listeners: new Set(),
    subscribe: (listener) => {
      store.listeners.add(listener);
      return () => store.listeners.delete(listener);
    },
    view: Object.create(i18n) as I18n,
    fixedT: new Map(),
  };
  // `languageChanged` also fires for a same-language resource reload (see
  // I18nProvider), which must re-render with the new strings too.
  i18n.on("languageChanged", () => {
    store.revision += 1;
    // Reads delegate to the live instance; only the identity is new.
    store.view = Object.create(i18n) as I18n;
    store.fixedT.clear();
    for (const listener of store.listeners) listener();
  });
  stores.set(i18n, store);
  return store;
}

/**
 * Project alias for react-i18next's `useTranslation`, with the same result
 * (`t`, `i18n`, `ready`) and re-render on a language change. Use the selector
 * form when calling: t($ => $.signin.title)
 *
 * react-i18next's hook copies every property descriptor of the i18next
 * instance into a fresh wrapper for each component that mounts it and
 * rebuilds its options on every render; opening a long issue mounts hundreds
 * of components that call it (MUL-9538). Resources are loaded synchronously
 * (`createI18n`), so there is no readiness or namespace loading to track:
 * `t` and the `i18n` identity change with the language, shared by every
 * component.
 */
// Without an i18next instance (a component rendered outside I18nProvider),
// react-i18next answers with the key, or nothing for a selector.
const notReadyT: AnyT = (key: unknown, options?: unknown) => {
  if (typeof options === "string") return options;
  const fallback = (options as { defaultValue?: unknown } | undefined)?.defaultValue;
  if (typeof fallback === "string") return fallback;
  if (typeof key === "function") return "";
  if (Array.isArray(key)) {
    const last: unknown = key[key.length - 1];
    return typeof last === "function" ? "" : last;
  }
  return key;
};
const noInstance = Object.assign([notReadyT, {} as I18n, false] as [AnyT, I18n, boolean], {
  t: notReadyT,
  i18n: {} as I18n,
  ready: false,
});
const noInstanceStore = { subscribe: () => () => {}, revision: 0 };

function useTranslationShared(ns?: string) {
  const context = useContext(I18nContext);
  const i18n = context?.i18n ?? (getI18n() as I18n | undefined);
  const store = i18n ? storeFor(i18n) : noInstanceStore;
  const revision = useSyncExternalStore(store.subscribe, () => store.revision, () => store.revision);
  const defaultNS = (context as { defaultNS?: string } | undefined)?.defaultNS;
  const namespace = ns ?? defaultNS ?? (i18n?.options.defaultNS as string | undefined) ?? "translation";
  return useMemo(() => {
    if (!i18n || !("fixedT" in store)) return noInstance;
    let t = store.fixedT.get(namespace);
    if (!t) {
      t = i18n.getFixedT(null, namespace as never) as unknown as AnyT;
      store.fixedT.set(namespace, t);
    }
    const result = [t, store.view, true] as [AnyT, I18n, boolean] & { t: AnyT; i18n: I18n; ready: boolean };
    result.t = t;
    result.i18n = store.view;
    result.ready = true;
    return result;
    // `revision` keys the memo: a language change swaps `t` and the view.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [i18n, namespace, revision]);
}

export const useT = useTranslationShared as unknown as typeof useTranslation;
