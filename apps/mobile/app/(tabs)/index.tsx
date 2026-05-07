import { ScrollView, Text } from "react-native";

export default function HomeScreen() {
  return (
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={{
        flexGrow: 1,
        justifyContent: "center",
        alignItems: "center",
        padding: 24,
      }}
    >
      <Text selectable style={{ fontSize: 32, fontWeight: "600" }}>
        你好 naiyuan
      </Text>
    </ScrollView>
  );
}
