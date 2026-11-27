// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { defaultStorage } from "./storage";

afterEach(() => { localStorage.clear(); });

it("enumerates stored entries rather than the Storage implementation's methods", () => {
  localStorage.setItem("preference:one", "first");
  localStorage.setItem("preference:two", "second");

  expect(defaultStorage.keys?.().sort()).toEqual(["preference:one", "preference:two"]);
});
