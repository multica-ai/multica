// Imported first by index.ts: core's persisted stores read `defaultStorage`
// when their modules are evaluated, so the backend must be installed before
// anything else pulls them in.
import * as SecureStore from "expo-secure-store";
import { Storage } from "expo-sqlite/kv-store";
import { installDefaultStorageBackend } from "@multica/core/platform/storage";
import {
  createMobileStorage,
  createSecureStore,
  forgetCredentialsFromPreviousInstall,
  type EnumerableKeyValueStore,
} from "./storage";

// Readable while locked after the first unlock, so background work can
// authenticate; never restored onto another device from a backup.
const keychainOptions: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY,
};

const general: EnumerableKeyValueStore = {
  getItem: (key) => Storage.getItemSync(key),
  setItem: (key, value) => Storage.setItemSync(key, value),
  removeItem: (key) => void Storage.removeItemSync(key),
  keys: () => Storage.getAllKeysSync(),
};

const secure = createSecureStore({
  getItem: (key) => SecureStore.getItem(key, keychainOptions),
  setItem: (key, value) => SecureStore.setItem(key, value, keychainOptions),
  deleteItemAsync: (key) => SecureStore.deleteItemAsync(key, keychainOptions),
});

forgetCredentialsFromPreviousInstall(general, secure);

export const mobileStorage = createMobileStorage(general, secure);

installDefaultStorageBackend(mobileStorage);
