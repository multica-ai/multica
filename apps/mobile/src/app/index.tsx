import { StatusBar, StyleSheet, useColorScheme } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { MobileProviders } from "../runtime/providers";
import { colors } from "../theme/tokens";

export default function MobileAppRoute() {
  const colorScheme = useColorScheme();
  const isDark = colorScheme === "dark";

  return (
    <SafeAreaProvider style={styles.root}>
      <StatusBar
        backgroundColor={colors.background}
        barStyle={isDark ? "light-content" : "dark-content"}
      />
      <MobileProviders />
    </SafeAreaProvider>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
});
