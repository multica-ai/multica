// @vitest-environment node
import { expect, it } from "vitest";
import { transformOtpPaste } from "./otp-paste";

it.each(["", "1", "12", "123", "12345", "000123"])("keeps batched native input %j", (text) => {
  expect(transformOtpPaste(text, 6)).toBe(text);
});

it("extracts the isolated complete code from a pasted message", () => {
  expect(transformOtpPaste("Your 6-digit Multica code is 012345.", 6)).toBe("012345");
  expect(transformOtpPaste("Code: 0123", 4)).toBe("0123");
});

it("does not extract a code from a longer number", () => {
  expect(transformOtpPaste("1234567", 6)).toBe("");
  expect(transformOtpPaste("Call 12345678", 6)).toBe("");
});
