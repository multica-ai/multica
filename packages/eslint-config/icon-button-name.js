// An icon-size Button shows a glyph and nothing else, so without a name a
// screen reader announces a bare "button" and tests can't find it by role.
// A tooltip doesn't count: it is a separate popup, not the button's name.
//
// A Button counts as named when it has `aria-label` or `aria-labelledby`, a
// props spread (the caller may pass the name), or content that may render
// text: a text node, an `sr-only` element, or any expression that isn't plain
// JSX. When the Button is the `render` element of a trigger, the trigger's own
// props and children reach the button too, so they count as well.
//
// Registered by the shared React config, so every package that extends it
// gets the rule without repeating it.

const NAME_ATTRIBUTES = new Set(["aria-label", "aria-labelledby"]);
const SR_ONLY = /(^|\s)sr-only(\s|$)/;

function attributeName(attr) {
  return attr.type === "JSXAttribute" && attr.name.type === "JSXIdentifier"
    ? attr.name.name
    : null;
}

function stringsIn(node) {
  if (!node) return [];
  switch (node.type) {
    case "Literal":
      return typeof node.value === "string" ? [node.value] : [];
    case "TemplateLiteral":
      return node.quasis.map((q) => q.value.raw);
    case "JSXExpressionContainer":
      return stringsIn(node.expression);
    case "ConditionalExpression":
      return [...stringsIn(node.consequent), ...stringsIn(node.alternate)];
    case "LogicalExpression":
      return [...stringsIn(node.left), ...stringsIn(node.right)];
    default:
      return [];
  }
}

function isIconSize(opening) {
  const size = opening.attributes.find((a) => attributeName(a) === "size");
  return !!size && stringsIn(size.value).some((s) => s.startsWith("icon"));
}

function namesItself(opening) {
  return opening.attributes.some(
    (a) => a.type === "JSXSpreadAttribute" || NAME_ATTRIBUTES.has(attributeName(a)),
  );
}

function isSrOnly(opening) {
  const className = opening.attributes.find((a) => attributeName(a) === "className");
  return !!className && stringsIn(className.value).some((s) => SR_ONLY.test(s));
}

function mayRenderText(node) {
  switch (node.type) {
    case "JSXText":
      return /\S/.test(node.value);
    case "JSXElement":
      return isSrOnly(node.openingElement) || node.children.some(mayRenderText);
    case "JSXFragment":
      return node.children.some(mayRenderText);
    case "JSXExpressionContainer":
      return mayRenderText(node.expression);
    case "JSXEmptyExpression":
      return false;
    case "ConditionalExpression":
      return mayRenderText(node.consequent) || mayRenderText(node.alternate);
    case "LogicalExpression":
      return (node.operator !== "&&" && mayRenderText(node.left)) || mayRenderText(node.right);
    case "Literal":
      return node.value !== null && typeof node.value !== "boolean" && String(node.value).trim() !== "";
    case "Identifier":
      return node.name !== "undefined";
    default:
      return true;
  }
}

// The element whose `render` prop is this Button, e.g. a TooltipTrigger.
function renderHost(element) {
  let node = element;
  while (node.parent.type === "ConditionalExpression" || node.parent.type === "LogicalExpression") {
    node = node.parent;
  }
  const container = node.parent;
  if (container.type !== "JSXExpressionContainer") return null;
  const attr = container.parent;
  if (attr.type !== "JSXAttribute" || attributeName(attr) !== "render") return null;
  return attr.parent.parent;
}

function isNamed(element) {
  if (namesItself(element.openingElement) || element.children.some(mayRenderText)) return true;
  const host = renderHost(element);
  return !!host && (namesItself(host.openingElement) || host.children.some(mayRenderText));
}

const iconButtonName = {
  meta: {
    type: "problem",
    docs: { description: "Require an accessible name on icon-size Buttons." },
    schema: [],
    messages: {
      unnamed:
        "Icon-only Button has no accessible name: screen readers announce a bare \"button\" and tests can't find it by role. Use IconButton from @multica/ui/components/common/icon-button — its `label` sets the name and the tooltip. In a row that repeats, keep the Button, add `aria-label`, and show the tooltip with DeferredTooltip.",
    },
  },
  create(context) {
    return {
      JSXElement(node) {
        const opening = node.openingElement;
        if (opening.name.type !== "JSXIdentifier" || opening.name.name !== "Button") return;
        if (!isIconSize(opening) || isNamed(node)) return;
        context.report({ node: opening, messageId: "unnamed" });
      },
    };
  },
};

export default {
  meta: { name: "@multica/eslint-config/a11y" },
  rules: { "icon-button-name": iconButtonName },
};
