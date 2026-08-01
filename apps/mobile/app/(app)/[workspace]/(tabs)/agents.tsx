import { View } from "react-native";
import AgentsPage from "../more/agents";
import { Header } from "@/components/ui/header";

export default function AgentsTab() {
  return (
    <View className="flex-1 bg-background">
      <Header title="Agents" />
      <View className="flex-1">
        <AgentsPage />
      </View>
    </View>
  );
}
