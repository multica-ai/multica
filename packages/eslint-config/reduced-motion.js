// Reduced motion has two sources: the system's `prefers-reduced-motion` and
// the Appearance page's Reduce motion switch. A bare media query string, read
// through `matchMedia` or a media-query hook, sees only the first, so the
// switch silently does nothing there. `packages/core/appearance` owns the one
// sanctioned query and is not covered by this rule.
//
// Matches the query as a string literal or a template literal anywhere, which
// covers `matchMedia("(prefers-reduced-motion: reduce)")`, `window.matchMedia?.(…)`
// and a query kept in a constant.
//
// Exported for `no-restricted-syntax`, whose options flat config replaces
// wholesale: a package that sets its own entries must list this one again.

const QUERY = "/^\\s*\\(\\s*prefers-reduced-motion/";

export const NO_SYSTEM_ONLY_REDUCED_MOTION = {
  selector: [
    `Literal[value=${QUERY}]`,
    `TemplateLiteral > TemplateElement:first-child[value.raw=${QUERY}]`,
  ].join(", "),
  message:
    "A prefers-reduced-motion query sees only the system setting, not the Appearance Reduce motion switch. Use useReducedMotion() in render or prefersReducedMotion() outside it, both from @multica/core/appearance; in CSS use the motion-reduce: variants (packages/ui/docs/motion.md).",
};
