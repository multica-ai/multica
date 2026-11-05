import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { ESLint } from "eslint";

const roots = [
  "vendor/tutti/packages/agent/gui",
  "packages/tutti-agent-gui-host/src",
  ...[
    "pages/local-conversation-flow.tsx",
    "pages/local-conversation-session-row.tsx",
    "pages/local-side-workspace.tsx",
    "pages/local-side-feedback.tsx",
    "pages/remote-folder-picker.tsx",
    "pages/local-conversation-import.tsx",
    "pages/local-runtime-notice.tsx",
    "pages/local-resource-preview.tsx",
    "components/tutti-agent-gui-panel.tsx",
    "platform/local-device.tsx"
  ].map((file) => `apps/desktop/src/renderer/src/${file}`)
];
const isFunction = (node) =>
  ts.isArrowFunction(node) ||
  ts.isFunctionExpression(node) ||
  ts.isFunctionDeclaration(node);
const callName = (node) =>
  ts.isCallExpression(node) ? node.expression.getText() : "";
const hasJsx = (node) =>
  ts.isJsxElement(node) ||
  ts.isJsxSelfClosingElement(node) ||
  ts.isJsxFragment(node) ||
  (ts.isCallExpression(node) &&
    /^(React\.)?createElement$/.test(callName(node))) ||
  Boolean(ts.forEachChild(node, hasJsx));

export function checkSource(source, filename = "component.tsx") {
  const file = ts.createSourceFile(
    filename,
    source,
    ts.ScriptTarget.Latest,
    true,
    filename.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  );
  const diagnostics = [];
  const observed = new Set();
  const declarations = [];
  const report = (node, code, message) => {
    const { line, character } = file.getLineAndCharacterOfPosition(
      node.getStart(file)
    );
    diagnostics.push({
      filename,
      line: line + 1,
      column: character + 1,
      code,
      message
    });
  };
  function collect(node) {
    if (
      ts.isCallExpression(node) &&
      /^(observer|observeClass)$/.test(callName(node)) &&
      ts.isIdentifier(node.arguments[0])
    )
      observed.add(node.arguments[0].text);
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name))
      declarations.push(node);
    ts.forEachChild(node, collect);
  }
  collect(file);
  function wrapped(node) {
    if (/^(observer|observeClass)$/.test(callName(node))) return true;
    return (
      /^(memo|React\.memo)$/.test(callName(node)) &&
      node.arguments[0] &&
      wrapped(node.arguments[0])
    );
  }
  function inRender(node) {
    let parent = node.parent;
    while (parent && !isFunction(parent) && !ts.isMethodDeclaration(parent))
      parent = parent.parent;
    return Boolean(parent);
  }
  function allocation(node) {
    if (
      ts.isCallExpression(node) &&
      ((ts.isPropertyAccessExpression(node.expression) &&
        /^(map|filter|slice|concat|flat|flatMap|bind)$/.test(
          node.expression.name.text
        )) ||
        /^(executionTargets|projectConnectorComposerItems)$/.test(
          callName(node)
        ))
    )
      return node;
    if (
      ts.isObjectLiteralExpression(node) ||
      ts.isArrayLiteralExpression(node) ||
      ts.isArrowFunction(node) ||
      ts.isFunctionExpression(node)
    )
      return node;
    if (
      ts.isParenthesizedExpression(node) ||
      ts.isAsExpression(node) ||
      ts.isSatisfiesExpression(node) ||
      ts.isNonNullExpression(node)
    )
      return allocation(node.expression);
    if (ts.isConditionalExpression(node))
      return allocation(node.whenTrue) || allocation(node.whenFalse);
    if (ts.isBinaryExpression(node))
      return allocation(node.left) || allocation(node.right);
    return null;
  }
  function visit(node) {
    if (
      ts.isCallExpression(node) &&
      callName(node) === "observer" &&
      node.arguments[0] &&
      (wrapped(node.arguments[0]) ||
        /^(memo|React\.memo)$/.test(callName(node.arguments[0])))
    )
      report(
        node,
        "double-observer",
        "observer already memoizes the component; keep custom memo outside observer."
      );
    if (
      ts.isFunctionDeclaration(node) &&
      node.name &&
      /^[A-Z]/.test(node.name.text) &&
      node.body &&
      (hasJsx(node.body) || node.type?.getText(file) === "null") &&
      !observed.has(node.name.text)
    )
      report(
        node.name,
        "observer",
        "Wrap this conversation component in observer."
      );
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      /^[A-Z]/.test(node.name.text) &&
      node.initializer &&
      hasJsx(node.initializer) &&
      (isFunction(node.initializer) || ts.isCallExpression(node.initializer)) &&
      !wrapped(node.initializer)
    )
      report(
        node.name,
        "observer",
        "Wrap this conversation component in observer (inside memo when it has a comparator)."
      );
    if (
      ts.isClassDeclaration(node) &&
      node.name &&
      node.heritageClauses?.some((clause) =>
        clause.types.some((type) =>
          /^(React\.)?(Pure)?Component$/.test(type.expression.getText(file))
        )
      ) &&
      node.members.some((member) => member.name?.getText(file) === "render") &&
      !observed.has(node.name.text)
    )
      report(node.name, "observer", "Observe this class with mobx-react.");
    if (
      ts.isJsxAttribute(node) &&
      node.initializer &&
      ts.isJsxExpression(node.initializer) &&
      node.initializer.expression
    ) {
      const expression = node.initializer.expression;
      if (allocation(expression))
        report(
          expression,
          "stable-props",
          "Define prop data with useMemo and event callbacks with usePersistCallback before JSX; extract a component for list items."
        );
      if (ts.isIdentifier(expression)) {
        // Only follow the binding in this lexical scope; a same-named binding in
        // another component is unrelated to this prop.
        let scope = node.parent;
        while (scope && !isFunction(scope)) scope = scope.parent;
        const declaration = declarations.find(
          (binding) =>
            binding.name.text === expression.text &&
            scope &&
            binding.pos >= scope.pos &&
            binding.end <= scope.end &&
            (() => {
              let owner = binding.parent;
              while (owner && !isFunction(owner)) owner = owner.parent;
              return owner === scope;
            })()
        );
        if (
          declaration &&
          scope &&
          declaration.pos >= scope.pos &&
          declaration.end <= scope.end &&
          declaration.initializer &&
          inRender(declaration)
        ) {
          if (allocation(declaration.initializer))
            report(
              expression,
              "stable-props",
              "Memoize this render-owned prop value or persist its event callback."
            );
          if (
            /^(useCallback|React\.useCallback)$/.test(
              callName(declaration.initializer)
            ) &&
            /^on[A-Z]/.test(node.name.getText(file))
          )
            report(
              expression,
              "persist-callback",
              "Forward event callbacks through usePersistCallback; keep dependency-driven effects separate."
            );
        }
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(file);
  return diagnostics;
}

function sourceFiles(root) {
  if (fs.statSync(root).isFile()) return [root];
  return fs.readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    if (entry.name === "node_modules" || entry.name === "testing") return [];
    const filename = path.join(root, entry.name);
    return entry.isDirectory()
      ? sourceFiles(filename)
      : /\.tsx?$/.test(entry.name) &&
          !/(\.(test|spec|generated)\.|\.d\.ts$)/.test(entry.name)
        ? [filename]
        : [];
  });
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const files = roots.flatMap(sourceFiles);
  const diagnostics = files.flatMap((filename) =>
    checkSource(fs.readFileSync(filename, "utf8"), filename)
  );
  for (const diagnostic of diagnostics)
    console.error(
      `${diagnostic.filename}:${diagnostic.line}:${diagnostic.column} ${diagnostic.code}: ${diagnostic.message}`
    );
  if (diagnostics.length) process.exitCode = 1;
  else
    console.log(`Conversation render stability: ${files.length} files passed.`);
  const eslint = new ESLint({
    overrideConfigFile: fileURLToPath(
      new URL("./conversation-eslint.config.mjs", import.meta.url)
    )
  });
  const results = await eslint.lintFiles(roots);
  const formatter = await eslint.loadFormatter("stylish");
  const formatted = formatter.format(results);
  if (formatted) console.error(formatted);
  if (results.some((result) => result.errorCount > 0)) process.exitCode = 1;
}
