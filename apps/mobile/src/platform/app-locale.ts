import { getLocales } from "expo-localization";
import { resolveLocale } from "./locale";

export const appLocale = resolveLocale(getLocales());
