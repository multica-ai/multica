import { StyleSheet } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { MobileProviders } from "../runtime/providers";

export default function MobileAppRoute() {
  return (
    <SafeAreaProvider style={styles.root}>
      <MobileProviders />
    </SafeAreaProvider>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
});
