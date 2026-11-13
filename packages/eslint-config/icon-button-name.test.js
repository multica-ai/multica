import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { ESLint, Linter } from "eslint";
import tseslint from "typescript-eslint";
import a11yPlugin from "./icon-button-name.js";

const linter = new Linter();

function flagged(code) {
  const messages = linter.verify(
    code,
    [
      {
        files: ["**/*.tsx"],
        languageOptions: {
          parser: tseslint.parser,
          parserOptions: { ecmaFeatures: { jsx: true } },
        },
        plugins: { "multica-a11y": a11yPlugin },
        rules: { "multica-a11y/icon-button-name": "error" },
      },
    ],
    "fixture.tsx",
  );
  const fatal = messages.find((m) => m.fatal);
  if (fatal) throw new Error(`fixture failed to parse: ${fatal.message}`);
  return messages.length > 0;
}

const FLAGGED = [
  ["unnamed icon button", '<Button size="icon-sm"><X /></Button>'],
  ["self-closing icon button", '<Button size="icon" />'],
  ["icon size from a conditional", '<Button size={compact ? "icon-xs" : "icon-sm"}><X /></Button>'],
  ["icon chosen by a conditional child", "<Button size=\"icon\">{open ? <ChevronUp /> : <ChevronDown />}</Button>"],
  ["icon behind &&", "<Button size=\"icon\">{busy && <Spinner />}</Button>"],
  ["unnamed render-prop trigger", '<TooltipTrigger render={<Button size="icon-sm" />}><X /></TooltipTrigger>'],
  [
    "render-prop trigger with content inside the Button",
    '<TooltipTrigger render={<Button size="icon-sm"><X /></Button>} />',
  ],
  ["label only in a tooltip", '<Tooltip><TooltipTrigger render={<Button size="icon" />}><X /></TooltipTrigger><TooltipContent>Close</TooltipContent></Tooltip>'],
];

const ALLOWED = [
  ["aria-label", '<Button size="icon-sm" aria-label="Close"><X /></Button>'],
  ["translated aria-label", '<Button size="icon-sm" aria-label={t(($) => $.close)}><X /></Button>'],
  ["aria-labelledby", '<Button size="icon" aria-labelledby="title-id"><X /></Button>'],
  ["props spread", '<Button size="icon-sm" {...props}><X /></Button>'],
  ["text child", '<Button size="icon">Go</Button>'],
  ["expression child", '<Button size="icon-xs">{count}</Button>'],
  ["sr-only child", '<Button size="icon"><X /><span className="sr-only">{t(($) => $.close)}</span></Button>'],
  ["named render-prop trigger", '<TooltipTrigger render={<Button size="icon-sm" aria-label="More" />}><X /></TooltipTrigger>'],
  ["render-prop trigger carrying the name", '<DropdownMenuTrigger aria-label="More" render={<Button size="icon-sm" />}><X /></DropdownMenuTrigger>'],
  ["render-prop trigger with an sr-only child", '<TooltipTrigger render={<Button size="icon" />}><X /><span className="sr-only">Close</span></TooltipTrigger>'],
  ["non-icon size", '<Button size="sm"><X /></Button>'],
  ["no size", "<Button><X /></Button>"],
  ["IconButton", '<IconButton label="Close"><X /></IconButton>'],
];

const ICONS = 'import { ChevronDown, ChevronUp, X, X as XIcon } from "@multica/ui/icons";\n';

const NATIVE_FLAGGED = [
  ["icon-only native button", '<button type="button"><X /></button>'],
  ["aliased icon import", '<button type="button"><XIcon className="size-4" /></button>'],
  ["namespace icon import", 'import * as Icons from "@multica/ui/icons";\n<button type="button"><Icons.X /></button>'],
  ["several icons", "<button><ChevronUp /><ChevronDown /></button>"],
  ["icon chosen by a conditional", "<button>{open ? <ChevronUp /> : <ChevronDown />}</button>"],
  ["icon behind &&", "<button>{open && <ChevronUp />}</button>"],
  ["icon inside a fragment", "<button><><X /></></button>"],
  ["native render-prop trigger with the icon inside", "<TooltipTrigger render={<button><X /></button>} />"],
  ["native render-prop trigger with the icon as a child", "<TooltipTrigger render={<button />}><X /></TooltipTrigger>"],
  [
    "native button with the label only in a tooltip",
    "<Tooltip><TooltipTrigger render={<button><X /></button>} /><TooltipContent>Close</TooltipContent></Tooltip>",
  ],
];

const NATIVE_ALLOWED = [
  ["native aria-label", '<button aria-label="Close"><X /></button>'],
  ["native translated aria-label", "<button aria-label={t(($) => $.close)}><X /></button>"],
  ["native aria-labelledby", '<button aria-labelledby="title-id"><X /></button>'],
  ["native props spread", "<button {...props}><X /></button>"],
  ["native icon and text", "<button><X /> Close</button>"],
  ["native icon and expression", "<button><X />{label}</button>"],
  ["native sr-only child", '<button><X /><span className="sr-only">Close</span></button>'],
  ["native child component that may render text", "<button><X /><Label /></button>"],
  ["native icon or text", "<button>{busy ? <X /> : label}</button>"],
  ["icon not from @multica/ui/icons", 'import { X } from "./x";\n<button><X /></button>'],
  ["empty native button", "<button />"],
  ["native named render-prop trigger", '<TooltipTrigger aria-label="Close" render={<button />}><X /></TooltipTrigger>'],
  ["native render-prop trigger with text", "<TooltipTrigger render={<button />}><X /> Close</TooltipTrigger>"],
];

const withIcons = (code) => (code.startsWith("import") ? code : ICONS + code);

for (const [name, code] of NATIVE_FLAGGED) {
  test(`flags ${name}`, () => assert.equal(flagged(withIcons(code)), true));
}

for (const [name, code] of NATIVE_ALLOWED) {
  test(`allows ${name}`, () => assert.equal(flagged(withIcons(code)), false));
}

for (const [name, code] of FLAGGED) {
  test(`flags ${name}`, () => assert.equal(flagged(code), true));
}

for (const [name, code] of ALLOWED) {
  test(`allows ${name}`, () => assert.equal(flagged(code), false));
}

test("the shared React config turns the rule on", async () => {
  const eslint = new ESLint({ cwd: fileURLToPath(new URL("../views", import.meta.url)) });
  const [result] = await eslint.lintText('export const A = () => <Button size="icon" />;\n', {
    filePath: "fixture.tsx",
  });
  assert.equal(result.messages.filter((m) => m.ruleId === "multica-a11y/icon-button-name").length, 1);
});
