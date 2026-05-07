import "react-native-get-random-values";
import { useEffect, useState } from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { CoreProvider } from "@multica/core/platform";
import { MOBILE_ENV } from "./env";
import { mobileStorage } from "../platform/storage";
import { RootNavigator } from "../navigation/root-navigator";
import { colors } from "../theme/tokens";

export function MobileProviders() {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let alive = true;
    mobileStorage.hydrate().finally(() => {
      if (alive) setReady(true);
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
    <CoreProvider
      apiBaseUrl={MOBILE_ENV.apiBaseUrl}
      wsUrl={MOBILE_ENV.wsUrl}
      storage={mobileStorage}
      fetchConfig={false}
      locale="en"
      resources={{}}
    >
      <RootNavigator />
    </CoreProvider>
  );
}

const styles = StyleSheet.create({
  loading: {
    alignItems: "center",
    backgroundColor: colors.background,
    flex: 1,
    justifyContent: "center",
  },
});
