// @vitest-environment node
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

describe("default Slack installation manifest", () => {
  it.each(["", ".zh", ".zh-Hant", ".ja", ".ko", ".fr"])(
    "keeps help available through mentions without registering /help (%s)",
    (locale) => {
      const source = readFileSync(
        path.resolve(__dirname, `../content/docs/slack-bot-integration${locale}.mdx`),
        "utf8",
      );
      const manifest = /^```yaml\r?\n([\s\S]*?)^```/m.exec(source)?.[1];
      if (!manifest) throw new Error("Missing Slack installation manifest");
      const commands = [...manifest.matchAll(/^\s+- command:\s*(\/\S+)/gm)].map(
        (match) => match[1],
      );
      expect(commands).not.toContain("/help");
      expect(commands).toEqual(expect.arrayContaining(["/issue", "/new", "/clear"]));
      expect(manifest).toMatch(/^\s+- app_mention$/m);
    },
  );
});
