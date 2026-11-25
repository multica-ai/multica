// Shared web/desktop code must not replace the whole document. On Desktop the
// main process's navigation guard cancels any document navigation that leaves
// the renderer entry, without an error or a visible sign: MUL-8964's Composio
// connect button sat on "Connecting…" because `window.location.href = url`
// went nowhere. jsdom tests and the web app never notice.
//
// Covers `location.href = …` and `location = …` assignments and
// `location.assign(…)` / `location.replace(…)`, on the bare global or through
// `window`, `document`, `globalThis` or `self`.
//
// Exported for `no-restricted-syntax`, whose options flat config replaces
// wholesale: a package that sets its own entries must list this one again.

const GLOBAL = "/^(window|document|globalThis|self)$/";
const LOCATION = `:matches(Identifier[name='location'], MemberExpression[object.name=${GLOBAL}][property.name='location'])`;

export const NO_FULL_PAGE_NAVIGATION = {
  selector: [
    `AssignmentExpression > MemberExpression.left[property.name='href']:has(> ${LOCATION}.object)`,
    `AssignmentExpression > MemberExpression.left[object.name=${GLOBAL}][property.name='location']`,
    `CallExpression > MemberExpression.callee[property.name=/^(assign|replace)$/]:has(> ${LOCATION}.object)`,
  ].join(", "),
  message:
    "Full-page navigation is silently blocked on Desktop once it leaves the app. Open an external URL with openExternal from packages/views/platform/open-external (`webTarget: \"same-tab\"` keeps the web tab); move within the app with the navigation adapter (useNavigation). A deliberate full-page navigation takes an inline disable that says why.",
};
