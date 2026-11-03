// @vitest-environment node
import { expect, it } from "vitest";
import { isServicePreviewUrl, previewServiceHost } from "./service-preview-url";

it.each([
  ["http://localhost:3000", "localhost"], ["http://APP.localhost.", "localhost"],
  ["http://127.1", "127.0.0.1"], ["http://2130706433", "127.0.0.1"], ["http://0x7f000001", "127.0.0.1"],
  ["http://0.0.0.0", "0.0.0.0"], ["http://[::1]", "::1"], ["http://[::ffff:127.0.0.1]", "::ffff:7f00:1"],
  ["http://192.168.1.10:3000/path", "192.168.1.10"], ["https://8.8.8.8/", "8.8.8.8"],
  ["ws://[2001:db8::10]/", "2001:db8::10"], ["http://169.254.169.254/", "169.254.169.254"],
  ["https://ccn53rwonxso.feishu.cn/docx/test", null], ["http://localhost.example.org", null],
  ["http://198.18.0.235.example.org", null], ["https://example.org", null], ["invalid", null],
  ["file://127.0.0.1/x", null], ["http://user:password@127.0.0.1", null],
])("classifies the URL host and preserves its address: %s", (url, expected) => {
  expect(previewServiceHost(url!)).toBe(expected);
  expect(isServicePreviewUrl(url!)).toBe(expected !== null);
});
