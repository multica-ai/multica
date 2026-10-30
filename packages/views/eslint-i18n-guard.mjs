// The other half of the i18n surface (MUL-6850). User-visible copy reaches the
// screen through two doors `i18next/no-literal-string` never watches — a JSX
// attribute and a toast argument — which is why MUL-6838 had to hand-fix ~30
// such strings after they had already shipped. Widening the plugin to
// `mode: "jsx-only"` is not the answer: measured on this package it reports
// 2639 errors, because it validates every literal that appears anywhere inside
// JSX (`isColVisible("status")`, `{ month: "short" }`, enum keys) and only ~10
// of those are copy. So the two doors are pinned individually instead — the
// same technique apps/desktop uses to guard shell.openExternal and
// router.navigate.
//
// Selectors live here rather than inline in eslint.config.mjs so
// eslint-i18n-guard.test.ts can lint fixtures against the very same strings the
// build enforces. A guard that is only spot-checked by hand drifts back into a
// hole: the first iteration of this rule matched a bare `Literal` child only,
// so `toast.error(err instanceof Error ? err.message : "Failed to save")` and
// ``aria-label={`${overflow} more`}`` both sailed through it.

const COPY_ATTRIBUTE =
  "JSXAttribute[name.name=/^(placeholder|title|aria-label)$/]";

// A literal with a letter in it. `https?:`-prefixed values are URLs and example
// endpoints, never copy.
const BARE_COPY = "Literal[value=/[A-Za-z]/]:not([value=/^https?:/])";

// A template is copy only when one of its *static* chunks carries a letter:
// `` `${label} key ${index}` `` has the English word "key" baked in, while
// `` `${a}: ${b}` `` is pure interpolation of already-translated values and is
// left alone.
const BARE_COPY_TEMPLATE =
  "TemplateLiteral > TemplateElement[value.raw=/[A-Za-z]/]:not([value.raw=/^https?:/])";

// Both `toast("…")` and `toast.error("…")` / `.success` / `.warning`.
const TOAST =
  ":matches(CallExpression[callee.name='toast'], CallExpression[callee.object.name='toast'])";

// The copy is just as untranslated when it is the fallback half of a
// `err instanceof Error ? err.message : "Failed to save"` or a `msg || "…"`.
const FALLBACK = ":matches(ConditionalExpression, LogicalExpression)";

export const NO_UNTRANSLATED_ATTRIBUTES = {
  selector: [
    `${COPY_ATTRIBUTE} > ${BARE_COPY}`,
    `${COPY_ATTRIBUTE} > JSXExpressionContainer > ${BARE_COPY}`,
    `${COPY_ATTRIBUTE} > JSXExpressionContainer > ${BARE_COPY_TEMPLATE}`,
  ].join(", "),
  message:
    "User-visible text in placeholder/title/aria-label must come from useT() — a screen reader reads aria-label the way a sighted user reads a JSX child. Exempt technical values (a CLI command, a token prefix) with an inline eslint-disable and a reason.",
};

export const NO_UNTRANSLATED_TOAST = {
  selector: [
    `${TOAST} > ${BARE_COPY}`,
    `${TOAST} > ${BARE_COPY_TEMPLATE}`,
    `${TOAST} > ${FALLBACK} > ${BARE_COPY}`,
    `${TOAST} > ${FALLBACK} > ${BARE_COPY_TEMPLATE}`,
  ].join(", "),
  message:
    "Toast copy must come from useT(). Pass a translated string, not a literal — including the fallback half of a `err.message : \"…\"` ternary.",
};
