// Physical Tailwind utilities (`ml-2`, `left-0`, `text-right`) do not mirror
// under `dir="rtl"`; their logical forms (`ms-2`, `inset-s-0`, `text-end`)
// render identically in LTR and flip in RTL. See "RTL in code" in
// apps/docs/content/docs/developers/conventions.mdx.

const LOGICAL = {
  ml: "ms",
  mr: "me",
  pl: "ps",
  pr: "pe",
  "scroll-ml": "scroll-ms",
  "scroll-mr": "scroll-me",
  "scroll-pl": "scroll-ps",
  "scroll-pr": "scroll-pe",
  left: "inset-s",
  right: "inset-e",
  // Tailwind 4.2 keeps these as legacy aliases of inset-s / inset-e.
  start: "inset-s",
  end: "inset-e",
  "border-l": "border-s",
  "border-r": "border-e",
  "rounded-l": "rounded-s",
  "rounded-r": "rounded-e",
  "rounded-tl": "rounded-ss",
  "rounded-tr": "rounded-se",
  "rounded-bl": "rounded-es",
  "rounded-br": "rounded-ee",
  "text-left": "text-start",
  "text-right": "text-end",
  "float-left": "float-start",
  "float-right": "float-end",
};

// Spacing and inset utilities only count with a value, so `left` / `right`
// words and keys such as `left-panel` are not utilities.
const SPACED = /^(-?)(scroll-[mp][lr]|[mp][lr]|left|right|start|end)-(\d|\[|\(|px$|auto$|full$)/;
const EDGED = /^(border-[lr]|rounded-[tb]?[lr])(?:-|$)/;
const WHOLE = /^(text-left|text-right|float-left|float-right)$/;
// No logical form exists for these; they need an `rtl:` counterpart.
const PAIRED = /^(origin-(?:left|right|top-left|top-right|bottom-left|bottom-right)|bg-(?:gradient|linear)-to-[lr])$/;

// A half-width pull back centers an element whatever the direction.
const CENTERING = /(^|[\s"'`:!])(-translate-x-1\/2|-translate-1\/2|translate-x-\[-50%\]|-translate-x-\[50%\])(?=$|[\s"'`!])/;

// Splits a class into its variant chain and utility, ignoring colons
// inside brackets, without the `!` important modifier.
function split(token) {
  let depth = 0;
  let start = 0;
  for (let i = 0; i < token.length; i++) {
    const c = token[i];
    if (c === "[" || c === "(") depth++;
    else if (c === "]" || c === ")") depth--;
    else if (c === ":" && depth === 0) start = i + 1;
  }
  return {
    variants: token.slice(0, start).split(":").filter(Boolean),
    utility: token.slice(start).replace(/^!|!$/g, ""),
  };
}

// `context` is the surrounding class source (a whole JSX attribute or cn()
// call), so a translate in a sibling literal still counts as centering.
export function findPhysicalClasses(value, context = value) {
  const centered = CENTERING.test(context);
  const found = [];
  for (const token of value.split(/\s+/).filter(Boolean)) {
    const { variants, utility } = split(token);
    // An explicitly direction-scoped class is already deliberate.
    if (variants.includes("ltr") || variants.includes("rtl")) continue;
    if (centered && /^-?left-1\/2$/.test(utility)) continue;
    const spaced = utility.match(SPACED);
    if (spaced) {
      found.push({ token, kind: "physical", logical: LOGICAL[spaced[2]] });
      continue;
    }
    const edged = utility.match(EDGED) ?? utility.match(WHOLE);
    if (edged) {
      found.push({ token, kind: "physical", logical: LOGICAL[edged[1]] });
      continue;
    }
    if (PAIRED.test(utility) && !new RegExp(`(^|[\\s"'\`:])rtl:(?:[^\\s"'\`]*:)?!?${utility.split("-").slice(0, -1).join("-")}-`).test(context)) {
      found.push({ token, kind: "unpaired" });
    }
  }
  return found;
}

function classContext(node, sourceCode) {
  for (let current = node.parent; current; current = current.parent) {
    if (current.type === "JSXAttribute" || current.type === "CallExpression") {
      return sourceCode.getText(current);
    }
  }
  return null;
}

/** @type {import("eslint").Rule.RuleModule} */
const rule = {
  meta: {
    type: "problem",
    docs: { description: "Use logical Tailwind utilities so layouts mirror in RTL." },
    messages: {
      physical: "`{{token}}` does not mirror in RTL; use the `{{logical}}` form instead.",
      unpaired: "`{{token}}` has no logical form; add an `rtl:` counterpart in the same class list.",
    },
    schema: [],
  },
  create(context) {
    const check = (node, value) => {
      const surrounding = classContext(node, context.sourceCode) ?? value;
      for (const { token, kind, logical } of findPhysicalClasses(value, surrounding)) {
        context.report({ node, messageId: kind, data: { token, logical } });
      }
    };
    return {
      Literal(node) {
        if (typeof node.value === "string") check(node, node.value);
      },
      TemplateElement(node) {
        check(node, node.value.raw);
      },
    };
  },
};

export default {
  meta: { name: "multica-logical-direction" },
  rules: { "logical-direction": rule },
};
