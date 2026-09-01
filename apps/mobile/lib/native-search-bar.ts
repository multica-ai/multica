/**
 * Platform split for picker search.
 *
 * iOS: `headerSearchBarOptions` wires UISearchController into the Stack header.
 * Android: that API is a Toolbar SearchView with a different cancel/close
 * contract and no equivalent to `onCancelButtonPress`, so callers render the
 * inline fallback from `useNativeSearchBar` instead of the native header bar.
 */

export type NativeSearchBarHandlers = {
  onChangeText: (text: string) => void;
  onCancel: () => void;
  autoFocus?: boolean;
};

type HeaderSearchBarOptions = {
  placeholder: string;
  autoCapitalize: "none";
  hideWhenScrolling: false;
  autoFocus?: boolean;
  onChangeText: (e: { nativeEvent: { text: string } }) => void;
  onCancelButtonPress: () => void;
};

export function nativeSearchBarScreenOptions(
  os: string,
  placeholder: string,
  handlers: NativeSearchBarHandlers,
): { headerSearchBarOptions?: HeaderSearchBarOptions } {
  if (os !== "ios") return {};
  return {
    headerSearchBarOptions: {
      placeholder,
      autoCapitalize: "none",
      hideWhenScrolling: false,
      autoFocus: handlers.autoFocus,
      onChangeText: (e) => handlers.onChangeText(e.nativeEvent.text),
      onCancelButtonPress: handlers.onCancel,
    },
  };
}

export function usesInlineSearchField(os: string): boolean {
  return os !== "ios";
}
