import assert from "node:assert/strict";
import { test } from "node:test";
import postcss from "postcss";
import { scopeCssPlugin, scopeSelector } from "./scope-css.mjs";

test("document selectors become the root", () => {
  assert.equal(scopeSelector(":root"), ".multica-agent-gui");
  assert.equal(scopeSelector(":root[data-theme=\"dark\"]"), ".multica-agent-gui[data-theme=\"dark\"]");
  assert.equal(scopeSelector("html"), ".multica-agent-gui");
  assert.equal(scopeSelector("body"), ".multica-agent-gui");
  assert.equal(scopeSelector("html.multica-agent-gui .a"), ".multica-agent-gui .a");
  assert.equal(scopeSelector("html.multica-agent-gui[data-theme].dark"), ".multica-agent-gui[data-theme].dark");
  assert.equal(scopeSelector("html[data-x] body"), ".multica-agent-gui[data-x]");
});

test("other selectors are limited to the root", () => {
  assert.equal(scopeSelector(".text-sm"), ".multica-agent-gui .text-sm");
  assert.equal(scopeSelector("*"), ".multica-agent-gui *");
  assert.equal(scopeSelector(".html-preview"), ".multica-agent-gui .html-preview");
  assert.equal(scopeSelector(".body"), ".multica-agent-gui .body");
});

test("at-rules without selectors stay global and nested rules are not prefixed twice", async () => {
  const { css } = await postcss([scopeCssPlugin()]).process(
    "@keyframes a { from { top: 0 } } @font-face { font-family: x } @layer base { .b { color: red; .c { color: blue } } } @media print { .d { color: red } }",
    { from: undefined },
  );
  assert.match(css, /@keyframes a \{ from/);
  assert.match(css, /@layer base \{ \.multica-agent-gui \.b \{ color: red; \.c \{/);
  assert.match(css, /@media print \{ \.multica-agent-gui \.d/);
});
