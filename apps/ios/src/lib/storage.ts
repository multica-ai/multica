import { MMKV } from "react-native-mmkv";
import type { StorageAdapter } from "@multica/core/types/storage";

const mmkv = new MMKV({ id: "multica-ios" });

export const storage: StorageAdapter = {
  getItem: (key: string) => mmkv.getString(key) ?? null,
  setItem: (key: string, value: string) => mmkv.set(key, value),
  removeItem: (key: string) => mmkv.delete(key),
};
