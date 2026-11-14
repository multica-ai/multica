import { ActivityIndicator, Pressable, RefreshControl, ScrollView, StyleSheet, Switch, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { BottomFade } from "../../components/BottomFade";
import { Chip, ChipGroup } from "../../components/Chip";
import { FloatingSurface } from "../../components/Floating";
import { Group, GroupRow } from "../../components/Group";
import { PushHeader } from "../../components/Header";
import { Screen } from "../../components/Screen";
import { SectionTitle } from "../../components/SectionTitle";
import { Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { Icon } from "../../icons/Icon";
import { runLook, type AutopilotRunModel, type TriggerModel } from "../../model/autopilots";
import { useStrings } from "../../strings";
import { floatShadow, useTheme } from "../../theme";

export interface AutopilotModel {
  title: string;
  description: string | null;
  paused: boolean;
  assignee: AvatarActor;
  mode: string;
  project: string | null;
}

interface AutopilotViewProps {
  state: "loading" | "error" | "ready";
  autopilot: AutopilotModel | null;
  triggers: readonly TriggerModel[];
  runs: { state: "loading" | "error" | "ready"; items: readonly AutopilotRunModel[] };
  canWrite: boolean;
  starting?: boolean;
  refreshing?: boolean;
  onRefresh?: () => void;
  onBack: () => void;
  onRetry: () => void;
  onMore: () => void;
  onRunNow: () => void;
  onTogglePause: () => void;
  onAddTrigger: () => void;
  onTrigger: (trigger: TriggerModel) => void;
  onToggleTrigger: (trigger: TriggerModel, enabled: boolean) => void;
  onOpenRun: (run: AutopilotRunModel) => void;
}

export function AutopilotView(props: AutopilotViewProps) {
  const { state, autopilot, triggers, runs, canWrite, starting = false, refreshing = false, onRefresh, onBack, onRetry } = props;
  const strings = useStrings();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <Screen>
      <PushHeader
        onBack={onBack}
        backLabel={strings.common.back}
        actions={state === "ready" ? [{ icon: "MoreHorizontal", label: strings.common.more, onPress: props.onMore }] : []}
      />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" || !autopilot ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <>
          <ScrollView contentInsetAdjustmentBehavior="automatic"
            contentContainerStyle={[styles.content, canWrite && styles.withActions]}
            refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
          >
            <Txt variant="heading" style={styles.title}>
              {autopilot.title}
            </Txt>
            <ChipGroup>
              <Chip
                icon={<View style={[styles.dot, { backgroundColor: autopilot.paused ? palette.label3 : palette.success }]} />}
                label={autopilot.paused ? strings.autopilot.paused : strings.autopilot.active}
                onPress={canWrite ? props.onTogglePause : undefined}
              />
              <Chip icon={<Avatar actor={autopilot.assignee} size={18} />} label={autopilot.assignee.name} />
              <Chip icon={<Icon name="ListTodo" size={15} color={palette.label2} />} label={autopilot.mode} />
              {autopilot.project ? <Chip icon={<Icon name="FolderKanban" size={15} color={palette.label2} />} label={autopilot.project} /> : null}
            </ChipGroup>
            {autopilot.description ? (
              <Txt variant="body" style={styles.description}>
                {autopilot.description}
              </Txt>
            ) : null}

            <View style={styles.block}>
              <SectionTitle action={canWrite ? { label: strings.form.add, onPress: props.onAddTrigger } : undefined}>{strings.autopilot.triggers}</SectionTitle>
              {triggers.length === 0 ? (
                <Txt variant="callout" tone="tertiary">
                  {strings.autopilot.noTriggers}
                </Txt>
              ) : (
                <Group>
                  {triggers.map((trigger, index) => (
                    <GroupRow
                      key={trigger.id}
                      icon={trigger.icon}
                      label={trigger.label}
                      detail={trigger.detail}
                      last={index === triggers.length - 1}
                      onPress={canWrite ? () => props.onTrigger(trigger) : undefined}
                      right={
                        <Switch
                          value={trigger.enabled}
                          disabled={!canWrite}
                          onValueChange={(enabled) => props.onToggleTrigger(trigger, enabled)}
                          trackColor={{ true: palette.success, false: palette.fillStrong }}
                        />
                      }
                    />
                  ))}
                </Group>
              )}
            </View>

            <View style={styles.block}>
              <SectionTitle>{strings.autopilot.runs}</SectionTitle>
              {runs.state === "loading" ? (
                <Loading />
              ) : runs.state === "error" ? (
                <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
              ) : runs.items.length === 0 ? (
                <Txt variant="callout" tone="tertiary">
                  {strings.autopilot.noRuns}
                </Txt>
              ) : (
                runs.items.map((run) => {
                  const look = runLook(run.status, palette);
                  return (
                    <Pressable
                      key={run.id}
                      accessibilityRole={run.issueId ? "button" : undefined}
                      disabled={!run.issueId}
                      onPress={() => props.onOpenRun(run)}
                      style={({ pressed }) => [styles.run, pressed && { opacity: 0.6 }]}
                    >
                      <Icon name={look.icon} size={17} color={look.color} />
                      <View style={styles.flex}>
                        <Txt variant="callout">{run.when}</Txt>
                        <Txt variant="meta" tone={run.status === "failed" ? "destructive" : "secondary"} numberOfLines={2}>
                          {run.detail}
                        </Txt>
                      </View>
                      {run.issueId ? <Icon name="ChevronRight" size={16} color={palette.label3} /> : null}
                    </Pressable>
                  );
                })
              )}
            </View>
          </ScrollView>
          {canWrite ? (
            <>
              <BottomFade height={insets.bottom + 80} />
              <View style={[styles.actions, { paddingBottom: Math.max(insets.bottom - 8, 12) }]}>
                <Pressable accessibilityRole="button" onPress={props.onTogglePause} style={({ pressed }) => [styles.secondaryWrap, pressed && { opacity: 0.7 }]}>
                  <FloatingSurface style={styles.secondary}>
                    <Icon name={autopilot.paused ? "Play" : "Pause"} size={17} color={palette.label} />
                    <Txt variant="calloutStrong">{autopilot.paused ? strings.autopilot.resume : strings.autopilot.pause}</Txt>
                  </FloatingSurface>
                </Pressable>
                <Pressable
                  accessibilityRole="button"
                  disabled={starting}
                  onPress={props.onRunNow}
                  style={({ pressed }) => [styles.primary, { backgroundColor: palette.label, opacity: pressed ? 0.8 : 1 }, floatShadow(palette)]}
                >
                  {starting ? <ActivityIndicator color={palette.background} /> : <Icon name="Play" size={17} color={palette.background} />}
                  <Txt variant="calloutStrong" style={{ color: palette.background }}>
                    {strings.autopilot.runNow}
                  </Txt>
                </Pressable>
              </View>
            </>
          ) : null}
        </>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1, minWidth: 0 },
  content: { paddingHorizontal: 20, paddingTop: 4, paddingBottom: 60 },
  withActions: { paddingBottom: 140 },
  title: { marginTop: 4, marginBottom: 14 },
  dot: { width: 8, height: 8, borderRadius: 4, marginHorizontal: 3 },
  description: { marginTop: 16 },
  block: { marginTop: 28 },
  run: { flexDirection: "row", alignItems: "center", gap: 12, paddingVertical: 7 },
  actions: { position: "absolute", left: 16, right: 16, bottom: 0, flexDirection: "row", gap: 10 },
  secondaryWrap: { flex: 1 },
  secondary: { height: 52, borderRadius: 26, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8 },
  primary: { flex: 2, height: 52, borderRadius: 26, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8 },
});
