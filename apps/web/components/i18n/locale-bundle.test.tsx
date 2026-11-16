// @vitest-environment node
import type { ComponentType } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { SUPPORTED_LOCALES } from "@multica/core/i18n";
import { RESOURCES } from "@multica/views/locales";
import { describe, expect, it, vi } from "vitest";
import type { LocaleBundleProps } from "./locale-bundle";

const { loaders } = vi.hoisted(() => ({
  loaders: new Map<
    unknown,
    () => Promise<{ default: ComponentType<LocaleBundleProps> }>
  >(),
}));

vi.mock("next/dynamic", () => ({
  default: (load: () => Promise<{ default: ComponentType<LocaleBundleProps> }>) => {
    const Component = () => null;
    loaders.set(Component, load);
    return Component;
  },
}));

import { LocaleBundle } from "./locale-bundle";

describe("locale bundle selection", () => {
  for (const locale of SUPPORTED_LOCALES) {
    it(`loads only the shared ${locale} resources`, async () => {
      const children = vi.fn<LocaleBundleProps["children"]>((resources) => {
        expect(Object.keys(resources)).toEqual([locale]);
        // Shared entry identity ensures web and the aggregate parity tests
        // cover the same namespace registration, including future additions.
        expect(resources[locale]).toBe(RESOURCES[locale]);
        return <span>{locale}</span>;
      });
      const selected = LocaleBundle({ locale, children });
      const load = loaders.get(selected.type);
      expect(load).toBeDefined();
      const { default: Bundle } = await load!();
      expect(renderToStaticMarkup(<Bundle>{children}</Bundle>)).toBe(
        `<span>${locale}</span>`,
      );
      expect(children).toHaveBeenCalledOnce();
    });
  }
});
