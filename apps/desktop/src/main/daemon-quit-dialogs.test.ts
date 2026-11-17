// @vitest-environment node
import { describe, expect, it } from "vitest";
import { quitConfirmationOptions, quitFailureOptions } from "./daemon-quit-dialogs";

describe("daemon quit dialog copy", () => {
  it.each([
    [1, "1 run is active on this device."],
    [3, "3 runs are active on this device."],
    [null, "Could not confirm whether any runs are active on this device."],
  ])("distinguishes known run count %s from unknown state", (count, message) => {
    expect(quitConfirmationOptions("en-US", count)).toMatchObject({
      message, buttons: ["Cancel", "Stop now and quit"], defaultId: 0, cancelId: 0,
    });
  });

  it.each([
    ["zh-CN", "退出 Multica"],
    ["zh-SG", "退出 Multica"],
    ["zh-Hant", "結束 Multica"],
    ["zh-TW", "結束 Multica"],
    ["zh-HK", "結束 Multica"],
    ["zh-MO", "結束 Multica"],
    ["ja-JP", "Multica を終了"],
    ["ko-KR", "Multica 종료"],
    ["fr-FR", "Quit Multica"],
    ["", "Quit Multica"],
  ])("resolves OS language %s using the native menu locale policy", (language, title) => {
    const options = quitConfirmationOptions(language, 3);
    expect(options.title).toBe(title);
    expect(options.message).toContain("3");
    expect(options.message).not.toContain("Agent tasks");
  });

  it.each(["en", "zh-Hans", "zh-Hant", "ja", "ko"])("distinguishes stopped and unconfirmed daemon states in %s", (language) => {
    const stopped = quitFailureOptions(language, true);
    const unknown = quitFailureOptions(language, false);
    expect(stopped.message).not.toEqual(unknown.message);
    expect(stopped.detail).not.toEqual(unknown.detail);
    expect(stopped).toMatchObject({ defaultId: 0, cancelId: 0 });
    expect(unknown.buttons).toEqual(stopped.buttons);
  });

  it("explains the resulting cancellation state without asserting an unconfirmed kill", () => {
    expect(quitFailureOptions("en", true)).toMatchObject({
      message: "The daemon has stopped, but its runs could not be confirmed to have exited.",
      detail: expect.stringContaining("the daemon remains stopped"),
    });
    expect(quitFailureOptions("en", false)).toMatchObject({
      message: "Could not confirm whether the daemon and its runs have stopped.",
      detail: expect.stringContaining("automatic daemon restart disabled"),
    });
  });
});
