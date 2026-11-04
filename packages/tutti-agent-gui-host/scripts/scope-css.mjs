// The GUI's stylesheet shares a document with the app. Every rule is limited to
// its root element, and the document-level selectors its sources were written
// for become that root, so the GUI keeps its environment and nothing leaks out.
const ROOT = ".multica-agent-gui";
const DOCUMENT_SELECTORS = /(?<![\w.#-])(?:html|body|:root|:host)(?![\w-])/g;
const NO_SELECTORS = new Set(["keyframes", "-webkit-keyframes", "font-face", "property", "page", "counter-style"]);

export function scopeSelector(selector) {
  // `html.x body` names one element twice once both become the root.
  const rewritten = selector.replace(DOCUMENT_SELECTORS, ROOT).replace(/(\.multica-agent-gui)((?:[.[:][^\s,>+~]*)?)\s+\.multica-agent-gui/g, "$1$2").replaceAll(`${ROOT}${ROOT}`, ROOT).replace(new RegExp(`(${ROOT.replace(".", "\\.")})\\.multica-agent-gui`, "g"), "$1");
  return rewritten.trimStart().startsWith(ROOT) ? rewritten : `${ROOT} ${rewritten.trim()}`;
}

export function scopeCssPlugin() {
  return {
    postcssPlugin: "multica-scope-agent-gui",
    Once(root) {
      root.walkRules((rule) => {
        for (let parent = rule.parent; parent; parent = parent.parent) {
          if (parent.type === "atrule" && NO_SELECTORS.has(parent.name)) return;
          // A nested selector is already inside its scoped parent.
          if (parent.type === "rule") return;
        }
        rule.selectors = rule.selectors.map(scopeSelector);
      });
    },
  };
}
scopeCssPlugin.postcss = true;
