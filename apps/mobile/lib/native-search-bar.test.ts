// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  nativeSearchBarScreenOptions,
  usesInlineSearchField,
} from "./native-search-bar";

describe("nativeSearchBarScreenOptions", () => {
  it("registers UISearchController options only on iOS", () => {
    const onChangeText = vi.fn();
    const onCancel = vi.fn();
    const ios = nativeSearchBarScreenOptions("ios", "Search people", {
      onChangeText,
      onCancel,
      autoFocus: true,
    });
    expect(ios.headerSearchBarOptions?.placeholder).toBe("Search people");
    expect(ios.headerSearchBarOptions?.autoFocus).toBe(true);
    ios.headerSearchBarOptions?.onChangeText({
      nativeEvent: { text: "ada" },
    } as never);
    expect(onChangeText).toHaveBeenCalledWith("ada");
    ios.headerSearchBarOptions?.onCancelButtonPress();
    expect(onCancel).toHaveBeenCalled();
  });

  it("omits the native search bar on Android so the inline field is used", () => {
    expect(
      nativeSearchBarScreenOptions("android", "Search people", {
        onChangeText: () => {},
        onCancel: () => {},
      }),
    ).toEqual({});
    expect(usesInlineSearchField("android")).toBe(true);
    expect(usesInlineSearchField("ios")).toBe(false);
  });
});
