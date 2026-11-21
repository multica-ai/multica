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
// A native `<button>` has no size to go by, so it is checked only when all of
// its content is icons imported from @multica/ui/icons, possibly chosen by a
// conditional. Any other child could render text, so the button is left alone.
//
// Registered by the shared React config, so every package that extends it
// gets the rule without repeating it.

const NAME_ATTRIBUTES = new Set(["aria-label", "aria-labelledby"]);
const SR_ONLY = /(^|\s)sr-only(\s|$)/;
const ICONS_MODULE = "@multica/ui/icons";

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

function importedIcons(program) {
  const names = new Set();
  const namespaces = new Set();
  for (const statement of program.body) {
    if (statement.type !== "ImportDeclaration" || statement.source.value !== ICONS_MODULE) continue;
    for (const specifier of statement.specifiers) {
      (specifier.type === "ImportNamespaceSpecifier" ? namespaces : names).add(specifier.local.name);
    }
  }
  return { names, namespaces };
}

function isIcon(opening, icons) {
  const { name } = opening;
  if (name.type === "JSXIdentifier") return icons.names.has(name.name);
  return (
    name.type === "JSXMemberExpression" &&
    name.object.type === "JSXIdentifier" &&
    icons.namespaces.has(name.object.name)
  );
}

// "icon" when the content shows only icons, "empty" when it shows nothing,
// "other" when it may show anything else.
function iconContent(nodes, icons) {
  const kinds = nodes.map((node) => contentKind(node, icons));
  if (kinds.includes("other")) return "other";
  return kinds.includes("icon") ? "icon" : "empty";
}

function contentKind(node, icons) {
  switch (node.type) {
    case "JSXText":
      return /\S/.test(node.value) ? "other" : "empty";
    case "JSXElement":
      return isIcon(node.openingElement, icons) ? "icon" : "other";
    case "JSXFragment":
      return iconContent(node.children, icons);
    case "JSXExpressionContainer":
      return contentKind(node.expression, icons);
    case "JSXEmptyExpression":
      return "empty";
    case "ConditionalExpression":
      return iconContent([node.consequent, node.alternate], icons);
    case "LogicalExpression":
      return iconContent(node.operator === "&&" ? [node.right] : [node.left, node.right], icons);
    case "Literal":
      return node.value === null || typeof node.value === "boolean" || node.value === "" ? "empty" : "other";
    case "Identifier":
      return node.name === "undefined" ? "empty" : "other";
    default:
      return "other";
  }
}

function isIconOnlyNativeButton(element, icons) {
  const host = renderHost(element);
  const content = host ? [...element.children, ...host.children] : element.children;
  return iconContent(content, icons) === "icon";
}

const iconButtonName = {
  meta: {
    type: "problem",
    docs: { description: "Require an accessible name on icon-size Buttons and icon-only native buttons." },
    schema: [],
    messages: {
      unnamed:
        "Icon-only Button has no accessible name: screen readers announce a bare \"button\" and tests can't find it by role. Use IconButton from @multica/ui/components/common/icon-button — its `label` sets the name and the tooltip. In a row that repeats, keep the Button, add `aria-label`, and show the tooltip with DeferredTooltip.",
    },
  },
  create(context) {
    let icons = { names: new Set(), namespaces: new Set() };
    return {
      Program(program) {
        icons = importedIcons(program);
      },
      JSXElement(node) {
        const opening = node.openingElement;
        if (opening.name.type !== "JSXIdentifier") return;
        if (opening.name.name === "Button") {
          if (!isIconSize(opening)) return;
        } else if (opening.name.name !== "button" || !isIconOnlyNativeButton(node, icons)) {
          return;
        }
        if (isNamed(node)) return;
        context.report({ node: opening, messageId: "unnamed" });
      },
    };
  },
};

export default {
  meta: { name: "@multica/eslint-config/a11y" },
  rules: { "icon-button-name": iconButtonName },
};
