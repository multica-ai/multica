// @vitest-environment node
import { expect, it } from "vitest";
import { aiSessionPreview } from "./preview";
import { referenceMessage } from "./references";

it("keeps readable AI summaries, code identifiers and reference labels", () => {
  expect(aiSessionPreview("## **已完成**\n\n- 调整 `some_file.ts`，保留 [详情](https://example.com)")).toBe("已完成 调整 some_file.ts，保留 详情");
  expect(aiSessionPreview("```ts\nconst some_value = 1;\n```\n下一步")).toBe("const some_value = 1; 下一步");
  const ref = { kind: "issue" as const, id: "task", name: "MUL-1 · 修复 UI" };
  expect(aiSessionPreview(referenceMessage("**继续处理**", [ref]))).toBe("继续处理");
  expect(aiSessionPreview(referenceMessage("", [ref]))).toBe(ref.name);
});
