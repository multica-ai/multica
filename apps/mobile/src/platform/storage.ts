import AsyncStorage from "@react-native-async-storage/async-storage";
import * as SecureStore from "expo-secure-store";
import type { StorageAdapter } from "@multica/core/types";

const TOKEN_KEY = "multica_token";

export class MobileStorage implements StorageAdapter {
  private cache = new Map<string, string>();

  async hydrate() {
    const keys = await AsyncStorage.getAllKeys();
    const pairs = await AsyncStorage.multiGet([...keys]);
    for (const [key, value] of pairs) {
      if (value !== null) this.cache.set(key, value);
    }

    const token = await SecureStore.getItemAsync(TOKEN_KEY);
    if (token) this.cache.set(TOKEN_KEY, token);
  }

  getItem(key: string) {
    return this.cache.get(key) ?? null;
  }

  setItem(key: string, value: string) {
    this.cache.set(key, value);
    if (key === TOKEN_KEY) {
      void SecureStore.setItemAsync(key, value);
      return;
    }
    void AsyncStorage.setItem(key, value);
  }

  removeItem(key: string) {
    this.cache.delete(key);
    if (key === TOKEN_KEY) {
      void SecureStore.deleteItemAsync(key);
      return;
    }
    void AsyncStorage.removeItem(key);
  }
}

export const mobileStorage = new MobileStorage();
