import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { RuleTester } from "eslint";
import tseslint from "typescript-eslint";
import plugin, { findPhysicalClasses } from "./logical-direction.js";

const tokens = (value, context) => findPhysicalClasses(value, context).map((f) => f.token);

describe("findPhysicalClasses", () => {
  it("reports physical utilities with or without variants and modifiers", () => {
    assert.deepEqual(
      tokens("ml-2 pr-4 hover:ml-auto [&>svg]:mr-1.5 !pl-0 -left-2 md:-right-[3px] border-l border-r-2 rounded-tl-md text-right"),
      ["ml-2", "pr-4", "hover:ml-auto", "[&>svg]:mr-1.5", "!pl-0", "-left-2", "md:-right-[3px]", "border-l", "border-r-2", "rounded-tl-md", "text-right"],
    );
  });

  it("names the logical replacement, including for the legacy start / end aliases", () => {
    const logical = Object.fromEntries(
      findPhysicalClasses("left-0 right-2 start-0 -end-1 text-left rounded-br-lg").map((f) => [f.token, f.logical]),
    );
    assert.deepEqual(logical, {
      "left-0": "inset-s",
      "right-2": "inset-e",
      "start-0": "inset-s",
      "-end-1": "inset-e",
      "text-left": "text-start",
      "rounded-br-lg": "rounded-ee",
    });
  });

  it("accepts logical utilities and words that are not utilities", () => {
    assert.deepEqual(
      tokens("ms-2 pe-4 inset-s-0 inset-e-3 text-start border-s rounded-ee col-start-2 row-end-1 self-start items-end"),
      [],
    );
    assert.deepEqual(tokens("left right left-panel arrow-left chevron-right align-left pr-review inline-end"), []);
  });

  it("allows direction-neutral centering in every translate form, also from a sibling literal", () => {
    for (const translate of ["-translate-x-1/2", "!-translate-x-1/2", "-translate-1/2", "translate-x-[-50%]", "-translate-x-[50%]"]) {
      assert.deepEqual(tokens(`fixed left-1/2 ${translate}`), [], translate);
    }
    assert.deepEqual(tokens("left-1/2", 'cn("left-1/2", open && "-translate-x-1/2")'), []);
    assert.deepEqual(tokens("left-1/2"), ["left-1/2"]);
    assert.deepEqual(tokens("left-2 -translate-x-1/2"), ["left-2"]);
  });

  it("leaves explicitly direction-scoped classes alone", () => {
    assert.deepEqual(tokens("ltr:left-0 rtl:right-0 rtl:hover:ml-2"), []);
  });

  it("requires an rtl: counterpart for utilities without a logical form", () => {
    assert.deepEqual(tokens("origin-right bg-gradient-to-l"), ["origin-right", "bg-gradient-to-l"]);
    assert.deepEqual(tokens("origin-right rtl:origin-left bg-gradient-to-l rtl:bg-gradient-to-r"), []);
  });
});

describe("multica/logical-direction", () => {
  const tester = new RuleTester({
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
  });

  it("checks JSX attributes, cn() calls and templates", () => {
    tester.run("logical-direction", plugin.rules["logical-direction"], {
      valid: [
        { code: '<div className="ms-2 inset-e-0 text-end" />' },
        { code: 'cn("left-1/2", centered && "-translate-x-1/2")' },
        { code: "<div className={`ps-${size} rtl:origin-left origin-right`} />" },
      ],
      invalid: [
        {
          code: '<div className="ml-2 text-right" />',
          errors: [
            { messageId: "physical", data: { token: "ml-2", logical: "ms" } },
            { messageId: "physical", data: { token: "text-right", logical: "text-end" } },
          ],
        },
        {
          code: 'cn("absolute", open ? "end-0" : "start-0")',
          errors: [
            { messageId: "physical", data: { token: "end-0", logical: "inset-e" } },
            { messageId: "physical", data: { token: "start-0", logical: "inset-s" } },
          ],
        },
        {
          code: "<span className={`absolute ${side} bg-gradient-to-l`} />",
          errors: [{ messageId: "unpaired", data: { token: "bg-gradient-to-l" } }],
        },
      ],
    });
  });
});
