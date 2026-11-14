import { createContext, useContext } from "react";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import type { RootStackParamList } from "../navigation/types";

export type DetailTarget = { kind: "issue"; issueId: string } | { kind: "chat"; sessionId: string };

export interface DetailControls {
  /** What the detail column shows; null on one column, where the stack shows it instead. */
  selected: DetailTarget | null;
  open: (target: DetailTarget) => void;
}

export const DetailContext = createContext<DetailControls | null>(null);

export type RootNavigation = NativeStackNavigationProp<RootStackParamList>;

export function pushDetail(navigation: RootNavigation, target: DetailTarget) {
  if (target.kind === "issue") navigation.navigate("Issue", { issueId: target.issueId });
  else navigation.navigate("Chat", { sessionId: target.sessionId });
}

/** Opens an issue or a chat: beside the list when there is room, else as a pushed screen. */
export function useDetail(): DetailControls {
  const navigation = useNavigation<RootNavigation>();
  const split = useContext(DetailContext);
  return split ?? { selected: null, open: (target) => pushDetail(navigation, target) };
}
