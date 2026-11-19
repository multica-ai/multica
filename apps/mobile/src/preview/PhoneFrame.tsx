import type { ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import { SafeAreaFrameContext, SafeAreaInsetsContext } from "react-native-safe-area-context";
import Svg, { Path, Rect } from "react-native-svg";
import { SchemeOverride, useTheme } from "../ui/theme";

// iPhone 16 Pro in points.
const WIDTH = 393;
const HEIGHT = 852;
const INSETS = { top: 54, bottom: 34, left: 0, right: 0 };
const FRAME = { x: 0, y: 0, width: WIDTH, height: HEIGHT };

function StatusBar() {
  const palette = useTheme();
  return (
    <View pointerEvents="none" style={styles.status}>
      <Text style={[styles.time, { color: palette.label }]}>9:41</Text>
      <View style={styles.island} />
      <View style={styles.sys}>
        <Svg width={18} height={12} viewBox="0 0 18 12">
          {[0, 5, 10, 15].map((x, i) => (
            <Rect key={x} x={x} y={8 - i * 2.6} width={3} height={4 + i * 2.6} rx={1} fill={palette.label} />
          ))}
        </Svg>
        <Svg width={16} height={12} viewBox="0 0 16 12">
          <Path d="M8 11.5 5.6 9a3.4 3.4 0 0 1 4.8 0z M3.4 6.8a6.5 6.5 0 0 1 9.2 0l-1.4 1.4a4.5 4.5 0 0 0-6.4 0z M1.2 4.6a9.6 9.6 0 0 1 13.6 0l-1.4 1.4a7.6 7.6 0 0 0-10.8 0z" fill={palette.label} />
        </Svg>
        <View style={[styles.battery, { borderColor: palette.label }]}>
          <View style={[styles.charge, { backgroundColor: palette.label }]} />
        </View>
      </View>
    </View>
  );
}

function Device({ children }: { children: ReactNode }) {
  const palette = useTheme();
  return (
    <View style={[styles.device, { backgroundColor: palette.background, borderColor: palette.separator }]}>
      {children}
      <StatusBar />
      <View pointerEvents="none" style={[styles.home, { backgroundColor: palette.label }]} />
    </View>
  );
}

export function PhoneFrame({ scheme, children }: { scheme: "light" | "dark"; children: ReactNode }) {
  return (
    <SchemeOverride.Provider value={scheme}>
      <SafeAreaFrameContext.Provider value={FRAME}>
        <SafeAreaInsetsContext.Provider value={INSETS}>
          <Device>{children}</Device>
        </SafeAreaInsetsContext.Provider>
      </SafeAreaFrameContext.Provider>
    </SchemeOverride.Provider>
  );
}

const styles = StyleSheet.create({
  device: { width: WIDTH, height: HEIGHT, borderRadius: 54, overflow: "hidden", borderWidth: 1 },
  status: { position: "absolute", top: 0, left: 0, right: 0, height: 54, flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingLeft: 46, paddingRight: 30, paddingTop: 6 },
  time: { fontSize: 16, fontWeight: "600", width: 60 },
  island: { position: "absolute", top: 11, left: (WIDTH - 124) / 2, width: 124, height: 36, borderRadius: 20, backgroundColor: "#000" },
  sys: { flexDirection: "row", alignItems: "center", gap: 6 },
  battery: { width: 26, height: 12, borderRadius: 4, borderWidth: 1.5, padding: 1.5, opacity: 0.9 },
  charge: { flex: 1, marginRight: 4, borderRadius: 1.5 },
  home: { position: "absolute", bottom: 8, left: (WIDTH - 136) / 2, width: 136, height: 5, borderRadius: 3 },
});
