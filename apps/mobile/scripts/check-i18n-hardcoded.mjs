import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const MOBILE_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const SOURCE_ROOTS = ["app", "components", "lib", "data/mutations"];
const USER_TEXT_PROPERTIES = new Set([
  "accessibilityHint",
  "accessibilityLabel",
  "alt",
  "cancelButtonTitle",
  "description",
  "destructiveButtonTitle",
  "emptyMessage",
  "emptyTitle",
  "headerBackTitle",
  "headerTitle",
  "label",
  "message",
  "placeholder",
  "pillLabel",
  "subtitle",
  "text",
  "title",
]);

// These strings are examples, identifiers, or brand names rather than prose.
const ALLOWED_LITERALS = new Set([
  "Multica",
  "Multica (Dev)",
  "Multica (Staging)",
  "&quot;",
  "you@example.com",
  "https://github.com/owner/repo",
  "formSheet",
  "@${m.name}",
  "@${marker.name}",
  "@${mention.name}",
  "${e.actor_type}:${e.actor_id}",
  "${named} +${remaining}",
  " · ${timeAgo(entry.resolved_at)}",
  "📦",
]);

function collectFiles(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    const stat = statSync(path);
    if (stat.isDirectory()) return collectFiles(path);
    if (!new Set([".ts", ".tsx"]).has(extname(path))) return [];
    if (path.includes(".test.") || path.includes("/__tests__/")) return [];
    return [path];
  });
}

function literalValue(node) {
  if (ts.isStringLiteralLike(node)) return node.text;
  return null;
}

function renderedValue(node, sourceFile) {
  return ts.isTemplateExpression(node)
    ? node.getText(sourceFile).slice(1, -1)
    : literalValue(node);
}

function isUserText(value) {
  if (!/[A-Za-z]/.test(value) || ALLOWED_LITERALS.has(value)) return false;
  if (/^(?:\/|\.\/|\.\.\/|https?:\/\/)/.test(value)) return false;
  if (/^[A-Z0-9_.:/+-]+$/.test(value)) return false;
  if (/^[a-z0-9_.:/-]+$/.test(value)) return false;
  return true;
}

const findings = [];

function report(sourceFile, node, kind, value) {
  if (!isUserText(value)) return;
  const position = sourceFile.getLineAndCharacterOfPosition(
    node.getStart(sourceFile),
  );
  findings.push({
    file: relative(MOBILE_ROOT, sourceFile.fileName),
    line: position.line + 1,
    kind,
    value,
  });
}

function propertyName(node) {
  if (ts.isIdentifier(node) || ts.isStringLiteralLike(node)) return node.text;
  return null;
}

function inspect(sourceFile, node) {
  if (ts.isJsxText(node)) {
    report(sourceFile, node, "JSX text", node.getText(sourceFile).trim());
  }

  if (ts.isJsxAttribute(node) && USER_TEXT_PROPERTIES.has(node.name.text)) {
    if (node.initializer && ts.isStringLiteral(node.initializer)) {
      report(
        sourceFile,
        node.initializer,
        `JSX ${node.name.text}`,
        node.initializer.text,
      );
    }
    if (
      node.initializer &&
      ts.isJsxExpression(node.initializer) &&
      node.initializer.expression
    ) {
      const visitAttributeExpression = (expression) => {
        if (
          ts.isCallExpression(expression) &&
          ts.isIdentifier(expression.expression) &&
          expression.expression.text === "translate"
        ) {
          return;
        }
        const value = renderedValue(expression, sourceFile);
        if (value !== null) {
          report(sourceFile, expression, `JSX ${node.name.text}`, value);
          return;
        }
        ts.forEachChild(expression, visitAttributeExpression);
      };
      visitAttributeExpression(node.initializer.expression);
    }
  }

  if (
    ts.isJsxExpression(node) &&
    !ts.isJsxAttribute(node.parent) &&
    node.expression
  ) {
    const visitRenderedExpression = (expression) => {
      if (
        ts.isCallExpression(expression) &&
        ts.isIdentifier(expression.expression) &&
        expression.expression.text === "translate"
      ) {
        return;
      }
      if (
        ts.isJsxElement(expression) ||
        ts.isJsxSelfClosingElement(expression) ||
        ts.isJsxFragment(expression) ||
        ts.isJsxAttribute(expression)
      ) {
        return;
      }
      const value = renderedValue(expression, sourceFile);
      if (value !== null) {
        report(sourceFile, expression, "JSX expression", value);
        return;
      }
      ts.forEachChild(expression, visitRenderedExpression);
    };
    visitRenderedExpression(node.expression);
  }

  if (
    (ts.isParameter(node) || ts.isBindingElement(node)) &&
    ts.isIdentifier(node.name) &&
    USER_TEXT_PROPERTIES.has(node.name.text) &&
    node.initializer
  ) {
    const value = literalValue(node.initializer);
    if (value !== null) {
      report(sourceFile, node.initializer, `default ${node.name.text}`, value);
    }
  }

  if (ts.isPropertyAssignment(node)) {
    const name = propertyName(node.name);
    if (name && USER_TEXT_PROPERTIES.has(name)) {
      const visitPropertyExpression = (expression) => {
        if (
          ts.isCallExpression(expression) &&
          ts.isIdentifier(expression.expression) &&
          expression.expression.text === "translate"
        ) {
          return;
        }
        const value = renderedValue(expression, sourceFile);
        if (value !== null) {
          report(sourceFile, expression, `property ${name}`, value);
          return;
        }
        ts.forEachChild(expression, visitPropertyExpression);
      };
      visitPropertyExpression(node.initializer);
    }
  }

  if (ts.isReturnStatement(node) && node.expression) {
    const value = literalValue(node.expression);
    if (value !== null)
      report(sourceFile, node.expression, "returned text", value);
  }

  if (
    ts.isVariableDeclaration(node) &&
    ts.isIdentifier(node.name) &&
    /(?:labels?|messages?|placeholders?|titles?|options)$/i.test(
      node.name.text,
    ) &&
    node.initializer
  ) {
    const visitNamedValue = (expression) => {
      if (
        ts.isCallExpression(expression) &&
        ts.isIdentifier(expression.expression) &&
        expression.expression.text === "translate"
      ) {
        return;
      }
      const value = renderedValue(expression, sourceFile);
      if (value !== null) {
        report(sourceFile, expression, `variable ${node.name.text}`, value);
        return;
      }
      ts.forEachChild(expression, visitNamedValue);
    };
    visitNamedValue(node.initializer);
  }

  if (
    ts.isCallExpression(node) &&
    ts.isPropertyAccessExpression(node.expression) &&
    node.expression.expression.getText(sourceFile) === "Alert" &&
    node.expression.name.text === "alert"
  ) {
    for (const argument of node.arguments.slice(0, 2)) {
      const visitAlertArgument = (expression) => {
        if (
          ts.isCallExpression(expression) &&
          ts.isIdentifier(expression.expression) &&
          expression.expression.text === "translate"
        ) {
          return;
        }
        const value = renderedValue(expression, sourceFile);
        if (value !== null) {
          report(sourceFile, expression, "Alert.alert", value);
          return;
        }
        ts.forEachChild(expression, visitAlertArgument);
      };
      visitAlertArgument(argument);
    }
  }

  if (
    ts.isCallExpression(node) &&
    ts.isIdentifier(node.expression) &&
    ["mapAuthError", "useNativeSearchBar"].includes(node.expression.text)
  ) {
    for (const argument of node.arguments) {
      const value = literalValue(argument);
      if (value !== null)
        report(sourceFile, argument, node.expression.text, value);
    }
  }

  ts.forEachChild(node, (child) => inspect(sourceFile, child));
}

for (const sourceRoot of SOURCE_ROOTS) {
  const directory = join(MOBILE_ROOT, sourceRoot);
  for (const file of collectFiles(directory)) {
    const text = readFileSync(file, "utf8");
    const scriptKind = file.endsWith(".tsx")
      ? ts.ScriptKind.TSX
      : ts.ScriptKind.TS;
    const sourceFile = ts.createSourceFile(
      file,
      text,
      ts.ScriptTarget.Latest,
      true,
      scriptKind,
    );
    inspect(sourceFile, sourceFile);
  }
}

if (process.argv.includes("--json")) {
  console.log(JSON.stringify(findings, null, 2));
  process.exit(0);
}

if (findings.length > 0) {
  console.error(
    "User-visible English literals must use the mobile i18n layer:",
  );
  for (const finding of findings) {
    console.error(
      `${finding.file}:${finding.line} [${finding.kind}] ${JSON.stringify(finding.value)}`,
    );
  }
  process.exit(1);
}

console.log("Mobile user-visible literal check passed.");
