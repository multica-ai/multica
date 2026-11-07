// @vitest-environment node
import { expect, it } from "vitest";
import { copyText } from "./clipboard";

it("returns false without a browser document or Clipboard API", async () => {
  expect(await copyText("synthetic payload")).toBe(false);
});
