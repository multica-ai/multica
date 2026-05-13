import * as SecureStore from "expo-secure-store";

export const TOKEN_KEY = "multica_token";

export const secureTokenStorage = {
  get: () => SecureStore.getItemAsync(TOKEN_KEY),
  set: (token: string) => SecureStore.setItemAsync(TOKEN_KEY, token),
  remove: () => SecureStore.deleteItemAsync(TOKEN_KEY),
};
