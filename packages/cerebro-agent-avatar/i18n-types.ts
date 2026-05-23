import "i18next";
import type en from "./locales/en.json";

// Declaration merging into the views-owned `I18nResources` interface so the
// `cerebro-agent-avatar` namespace participates in the selector form: a
// `useT("cerebro-agent-avatar")` call returns a `t` that knows about
// `generate_ai`, `generate_button`, etc. The shape is sourced from the
// English resource file; CI's locale parity test confirms zh-Hans matches.
declare global {
  interface I18nResources {
    "cerebro-agent-avatar": typeof en;
  }
}

// Side-effect-only module; importing this file pulls the augmentation into
// the consumer's TS program. Re-exported via the package entry so any
// downstream consumer (including views imports of this package's hooks)
// inherits the namespace typing automatically.
export {};
