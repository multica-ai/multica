// @vitest-environment node
import { describe, expect, it } from "vitest";
import { SUPPORTED_LOCALES } from "@multica/core/i18n";
import { RESOURCES } from "../locales";
import { getTestResources } from "./i18n";

describe("test locale resources", () => {
  for (const locale of SUPPORTED_LOCALES) {
    it(`loads every production namespace for ${locale} with the English fallback`, () => {
      const resources = getTestResources(locale);
      expect(Object.keys(resources).sort()).toEqual(
        [...new Set(["en", locale])].sort(),
      );
      expect(resources.en).toEqual(RESOURCES.en);
      expect(resources[locale]).toEqual(RESOURCES[locale]);
    });
  }
});
