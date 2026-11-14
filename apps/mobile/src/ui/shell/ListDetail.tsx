import { useContext, useEffect, useMemo, useRef, useState, type ComponentType, type ReactNode } from "react";
import { StyleSheet, View, useWindowDimensions } from "react-native";
import { SafeAreaInsetsContext, useSafeAreaInsets } from "react-native-safe-area-context";
import { useNavigation, useRoute, type NavigationProp } from "@react-navigation/native";
import { FoldLayoutView, type Fold, type FoldLayout } from "../../platform/fold-layout";
import { EmptyDetailHeader, HeaderPlacementContext } from "../components/Header";
import { Screen } from "../components/Screen";
import { Txt } from "../components/Text";
import { Icon, type IconName } from "../icons/Icon";
import type { TabParamList } from "../navigation/types";
import { ChatDetail } from "../screens/chat/ChatScreen";
import { IssueDetail } from "../screens/issue/IssueScreen";
import { useStrings } from "../strings";
import { useTheme } from "../theme";
import { DetailContext, pushDetail, type DetailControls, type DetailTarget, type RootNavigation } from "./detail";
import { listColumnWidth } from "./split";

const NO_FOLDS: readonly Fold[] = [];

type ListTab = "inbox" | "myIssues" | "agents";

const EMPTY_ICON: Record<ListTab, IconName> = { inbox: "Inbox", myIssues: "CircleUser", agents: "Bot" };

function targetOf(route: { name: string; params?: object }): DetailTarget | null {
  const params = route.params as { issueId?: string; sessionId?: string } | undefined;
  if (route.name === "Issue" && params?.issueId) return { kind: "issue", issueId: params.issueId };
  if (route.name === "Chat" && params?.sessionId) return { kind: "chat", sessionId: params.sessionId };
  return null;
}

/**
 * Keeps the stack and the detail column telling the same story when the window changes width,
 * as when iPhone Duo folds: an issue open beside the list is pushed on folding, and an issue
 * pushed over the list moves beside it on unfolding. One column has nothing to carry over.
 */
function useHandoff(wide: boolean, selected: DetailTarget | null, select: (target: DetailTarget | null) => void) {
  const navigation = useNavigation<NavigationProp<TabParamList>>();
  const tab = useRoute().name;
  const was = useRef(wide);
  useEffect(() => {
    if (was.current === wide) return;
    was.current = wide;
    const tabs = navigation.getState();
    const root = navigation.getParent<RootNavigation>();
    const current = tabs.routes[tabs.index]?.name === tab;
    if (!wide) {
      select(null);
      if (current && root && selected && root.getState().index === 0) pushDetail(root, selected);
      return;
    }
    if (!current || !root) return;
    const above = root.getState().routes.slice(1);
    const targets = above.map(targetOf);
    const top = targets.at(-1);
    if (top && targets.every(Boolean)) {
      select(top);
      root.popToTop();
    }
  }, [wide, selected, select, navigation, tab]);
}

function EmptyDetail({ tab, folds }: { tab: ListTab; folds: readonly Fold[] }) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <View style={styles.empty}>
      <EmptyDetailHeader />
      <Icon name={EMPTY_ICON[tab]} size={34} color={palette.label3} strokeWidth={1.6} />
      <Txt variant="callout" tone="tertiary" style={styles.emptyText}>
        {strings.detail[tab]}
      </Txt>
      {folds.length ? (
        <Txt variant="meta" tone="tertiary">
          {strings.detail.foldHint}
        </Txt>
      ) : null}
    </View>
  );
}

function Detail({ target, tab, folds }: { target: DetailTarget | null; tab: ListTab; folds: readonly Fold[] }) {
  if (!target) {
    return (
      <Screen>
        <EmptyDetail tab={tab} folds={folds} />
      </Screen>
    );
  }
  return target.kind === "issue" ? <IssueDetail key={target.issueId} issueId={target.issueId} /> : <ChatDetail key={target.sessionId} sessionId={target.sessionId} />;
}

/**
 * A list tab that shows its detail beside it when the window is wide, such as iPhone Duo unfolded.
 * The list keeps its controls in its own column; the detail's go to the navigation bar.
 */
function ListDetail({ tab, children }: { tab: ListTab; children: ReactNode }) {
  const palette = useTheme();
  const placement = useContext(HeaderPlacementContext);
  const insets = useSafeAreaInsets();
  const { width } = useWindowDimensions();
  const [layout, setLayout] = useState<FoldLayout | null>(null);
  const folds = layout?.folds ?? NO_FOLDS;
  const listWidth = listColumnWidth(width, folds);
  // The detail's composer sits on the bottom edge; under a horizontal tab bar, that edge is the bar's top.
  const bottom = Math.max(insets.bottom, layout?.safeArea.bottom ?? 0);
  const wide = listWidth !== null;
  const [selected, setSelected] = useState<DetailTarget | null>(null);
  const navigation = useNavigation<RootNavigation>();
  useHandoff(wide, selected, setSelected);

  const controls = useMemo<DetailControls>(
    () => ({ selected: wide ? selected : null, open: (target) => (wide ? setSelected(target) : pushDetail(navigation, target)) }),
    [wide, selected, navigation],
  );
  const listInsets = useMemo(() => (wide ? { ...insets, right: 0 } : insets), [wide, insets]);
  const detailInsets = useMemo(() => ({ ...insets, left: 0, bottom }), [insets, bottom]);

  return (
    <DetailContext.Provider value={controls}>
      <FoldLayoutView style={[styles.row, { backgroundColor: palette.background }]} onLayoutChange={setLayout}>
        <SafeAreaInsetsContext.Provider value={listInsets}>
          <HeaderPlacementContext.Provider value={wide ? "inline" : placement}>
            <View style={wide ? { width: listWidth } : styles.flex}>{children}</View>
          </HeaderPlacementContext.Provider>
        </SafeAreaInsetsContext.Provider>
        {wide ? (
          <>
            <View style={[styles.divider, { backgroundColor: palette.separator }]} />
            <SafeAreaInsetsContext.Provider value={detailInsets}>
              <HeaderPlacementContext.Provider value="detail">
                <View style={styles.flex}>
                  <Detail target={selected} tab={tab} folds={folds} />
                </View>
              </HeaderPlacementContext.Provider>
            </SafeAreaInsetsContext.Provider>
          </>
        ) : null}
      </FoldLayoutView>
    </DetailContext.Provider>
  );
}

/** Wraps a list tab's screen in `ListDetail`. */
export function withListDetail(tab: ListTab, ListScreen: ComponentType) {
  function ListDetailScreen() {
    return (
      <ListDetail tab={tab}>
        <ListScreen />
      </ListDetail>
    );
  }
  ListDetailScreen.displayName = `ListDetail(${tab})`;
  return ListDetailScreen;
}

const styles = StyleSheet.create({
  row: { flex: 1, flexDirection: "row" },
  flex: { flex: 1 },
  divider: { width: StyleSheet.hairlineWidth },
  empty: { flex: 1, alignItems: "center", justifyContent: "center", gap: 4, paddingHorizontal: 24 },
  emptyText: { marginTop: 8, textAlign: "center" },
});
