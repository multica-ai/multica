import "react-native-get-random-values";
import { useEffect, useState } from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { useAuthStore } from "@multica/core/auth";
import { CoreProvider } from "@multica/core/platform";
import { MOBILE_ENV } from "./env";
import {
  getMobileI18nResources,
  getInitialMobileLocale,
  getPersistedMobileLocale,
  MobileI18nProvider,
  MobileLocaleProvider,
  normalizeMobileLocale,
  useMobileLocale,
} from "../i18n/mobile-locale";
import { mobileStorage } from "../platform/storage";
import { RootNavigator } from "../navigation/root-navigator";
import { colors } from "../theme/tokens";

export function MobileProviders() {
  const [ready, setReady] = useState(false);
  const [locale, setLocale] = useState(() => getInitialMobileLocale());

  useEffect(() => {
    let alive = true;
    mobileStorage.hydrate().finally(() => {
      if (!alive) return;
      setLocale(getInitialMobileLocale());
      setReady(true);
    });
    return () => {
      alive = false;
    };
  }, []);

  if (!ready) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color={colors.foreground} />
      </View>
    );
  }

  return (
    <MobileLocaleProvider locale={locale} setLocale={setLocale}>
      <CoreProvider
        apiBaseUrl={MOBILE_ENV.apiBaseUrl}
        wsUrl={MOBILE_ENV.wsUrl}
        storage={mobileStorage}
        fetchConfig={false}
        locale={locale}
        resources={getMobileI18nResources()}
      >
        <MobileI18nProvider key={locale}>
          <MobileUserLocaleSync />
          <RootNavigator />
        </MobileI18nProvider>
      </CoreProvider>
    </MobileLocaleProvider>
  );
}

function MobileUserLocaleSync() {
  const userLanguage = useAuthStore((state) => state.user?.language ?? null);
  const { locale, setLocale } = useMobileLocale();

  useEffect(() => {
    if (!userLanguage) return;
    if (getPersistedMobileLocale()) return;
    const next = normalizeMobileLocale(userLanguage);
    if (next === locale) return;
    setLocale(next);
  }, [locale, setLocale, userLanguage]);

  return null;
}

const styles = StyleSheet.create({
  loading: {
    alignItems: "center",
    backgroundColor: colors.background,
    flex: 1,
    justifyContent: "center",
  },
});
