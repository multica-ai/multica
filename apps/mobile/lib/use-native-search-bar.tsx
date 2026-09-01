/**
 * Hook for wiring picker search into a route. Returns the current query
 * string plus an Android inline field.
 *
 * iOS: native `UISearchController` via react-native-screens
 * `headerSearchBarOptions`. Cancel must reset query in `onCancelButtonPress`
 * because the native Cancel button clears text without firing `onChangeText`.
 *
 * Android: `headerSearchBarOptions` is a Toolbar SearchView with a different
 * close contract, so we skip it and render `SearchField` in the route body.
 *
 * Requires the Stack.Screen to register `headerShown: true` + a `title` in
 * the layout. See `apps/mobile/app/(app)/[workspace]/_layout.tsx`.
 */
import { useCallback, useLayoutEffect, useState, type ReactElement } from "react";
import { Platform, TextInput, View } from "react-native";
import { useNavigation } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import {
  nativeSearchBarScreenOptions,
  usesInlineSearchField,
} from "@/lib/native-search-bar";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";

export function useNativeSearchBar(
  placeholder: string,
  options?: { autoFocus?: boolean },
): { query: string; SearchField: () => ReactElement | null } {
  const navigation = useNavigation();
  const [query, setQuery] = useState("");
  const autoFocus = options?.autoFocus;
  const os = Platform.OS;

  useLayoutEffect(() => {
    navigation.setOptions(
      nativeSearchBarScreenOptions(os, placeholder, {
        onChangeText: setQuery,
        onCancel: () => setQuery(""),
        autoFocus,
      }),
    );
  }, [navigation, placeholder, autoFocus, os]);

  const SearchField = useCallback((): ReactElement | null => {
    if (!usesInlineSearchField(os)) return null;
    return (
      <View className="flex-row items-center gap-2 border-b border-border px-4 py-2">
        <Ionicons name="search" size={18} color="#71717a" />
        <TextInput
          value={query}
          onChangeText={setQuery}
          placeholder={placeholder}
          placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
          autoFocus={autoFocus}
          autoCorrect={false}
          autoCapitalize="none"
          returnKeyType="search"
          className="flex-1 text-base text-foreground"
          style={{ fontSize: 16 }}
        />
      </View>
    );
  }, [os, query, placeholder, autoFocus]);

  return { query, SearchField };
}
