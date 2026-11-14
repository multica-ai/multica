// Imported first by boot.ts. Hermes has no Web Crypto, and core stamps every
// request with an id from crypto.randomUUID / getRandomValues, so without
// this no API call leaves the device.
import * as ExpoCrypto from "expo-crypto";
import { installCrypto } from "./crypto";

installCrypto(globalThis, ExpoCrypto);
