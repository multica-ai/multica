import { View } from "react-native";
import SettingsPage from "../more/settings";
import { Header } from "@/components/ui/header";

export default function SettingsTab() {
  return (
    <View className="flex-1 bg-background">
      <Header title="Settings" />
      <View className="flex-1">
        <SettingsPage />
      </View>
    </View>
  );
}
