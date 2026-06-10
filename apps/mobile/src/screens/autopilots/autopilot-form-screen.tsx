import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import {
  Alert,
  Clipboard,
  Keyboard,
  type KeyboardEvent,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import {
  autopilotDetailOptions,
  buildAutopilotWebhookUrl,
  useCreateAutopilot,
  useCreateAutopilotTrigger,
  useUpdateAutopilot,
  useUpdateAutopilotTrigger,
} from "@multica/core/autopilots";
import { api } from "@multica/core/api";
import { useCoreQuery } from "@multica/core/provider";
import type {
  AutopilotAssigneeType,
  AutopilotExecutionMode,
  AutopilotTrigger,
} from "@multica/core/types";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { Check, ChevronRight, CircleHelp, Rocket, Webhook } from "lucide-react-native";
import { Button, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";
import { AutopilotScheduleFields } from "./autopilot-schedule-fields";
import {
  AUTOPILOT_EVENT_FILTER_DOC_URL,
  getActorName,
  getDefaultTriggerConfig,
  getProjectTitle,
  parseCronExpression,
  parseEventFilters,
  stringifyEventFilters,
  toCronExpression,
  type TriggerFormConfig,
} from "./autopilot-mobile-utils";

type Props = NativeStackScreenProps<RootStackParamList, "AutopilotForm">;
type AssigneeSelection = { id: string; type: AutopilotAssigneeType };
type KeyboardFrame = { height: number; screenY: number };
type PickerItem<T extends string> = {
  detail?: string;
  icon?: React.ReactNode;
  label: string;
  value: T;
};

const FOCUS_SCROLL_DELAY_MS = 220;
const KEYBOARD_SCROLL_GAP = spacing.lg;

function useKeyboardFrame(): KeyboardFrame {
  const { height: windowHeight } = useWindowDimensions();
  const [frame, setFrame] = useState<KeyboardFrame>({
    height: 0,
    screenY: windowHeight,
  });

  useEffect(() => {
    const showEvent = Platform.OS === "ios" ? "keyboardWillChangeFrame" : "keyboardDidShow";
    const hideEvent = Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide";

    function show(event: KeyboardEvent) {
      const screenY = event.endCoordinates.screenY;
      const heightFromScreen = Math.max(0, windowHeight - screenY);
      const height = heightFromScreen || event.endCoordinates.height;
      setFrame({
        height,
        screenY: heightFromScreen > 0 ? screenY : windowHeight - height,
      });
    }

    function hide() {
      setFrame({ height: 0, screenY: windowHeight });
    }

    const showSubscription = Keyboard.addListener(showEvent, show);
    const hideSubscription = Keyboard.addListener(hideEvent, hide);

    return () => {
      showSubscription.remove();
      hideSubscription.remove();
    };
  }, [windowHeight]);

  useEffect(() => {
    if (frame.height === 0) {
      setFrame({ height: 0, screenY: windowHeight });
    }
  }, [frame.height, windowHeight]);

  return frame;
}

export function AutopilotFormScreen({ navigation, route }: Props) {
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  const keyboardFrame = useKeyboardFrame();
  const { workspace } = useMobileWorkspace();
  const autopilotId = route.params?.autopilotId;
  const isEdit = Boolean(autopilotId);
  const { data, isLoading } = useCoreQuery({
    ...autopilotDetailOptions(workspace.id, autopilotId ?? ""),
    enabled: Boolean(autopilotId),
  });
  const { data: agents = [] } = useCoreQuery(agentListOptions(workspace.id));
  const { data: squads = [] } = useCoreQuery(squadListOptions(workspace.id));
  const { data: projects = [] } = useCoreQuery(projectListOptions(workspace.id));
  const createAutopilot = useCreateAutopilot();
  const updateAutopilot = useUpdateAutopilot();
  const createTrigger = useCreateAutopilotTrigger();
  const updateTrigger = useUpdateAutopilotTrigger();

  const initializedIdRef = useRef<string | null>(null);
  const scrollViewRef = useRef<ScrollView>(null);
  const scrollYRef = useRef(0);
  const titleInputRef = useRef<TextInput | null>(null);
  const promptInputRef = useRef<TextInput | null>(null);
  const cronInputRef = useRef<TextInput | null>(null);
  const eventFiltersInputRef = useRef<TextInput | null>(null);
  const activeInputRef = useRef<RefObject<TextInput | null> | null>(null);
  const keyboardFrameRef = useRef(keyboardFrame);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [assignee, setAssignee] = useState<AssigneeSelection | null>(null);
  const [executionMode, setExecutionMode] = useState<AutopilotExecutionMode>("create_issue");
  const [projectId, setProjectId] = useState<string | null>(null);
  const [triggerKind, setTriggerKind] = useState<"schedule" | "webhook">("schedule");
  const [triggerConfig, setTriggerConfig] = useState<TriggerFormConfig>(getDefaultTriggerConfig);
  const [eventFiltersText, setEventFiltersText] = useState("");
  const [assigneePickerOpen, setAssigneePickerOpen] = useState(false);
  const [projectPickerOpen, setProjectPickerOpen] = useState(false);
  const [createdWebhookTrigger, setCreatedWebhookTrigger] = useState<AutopilotTrigger | null>(null);

  useEffect(() => {
    if (!autopilotId || !data || initializedIdRef.current === autopilotId) return;
    initializedIdRef.current = autopilotId;
    setTitle(data.autopilot.title);
    setDescription(data.autopilot.description ?? "");
    setAssignee({
      id: data.autopilot.assignee_id,
      type: data.autopilot.assignee_type ?? "agent",
    });
    setExecutionMode(data.autopilot.execution_mode);
    setProjectId(data.autopilot.project_id ?? null);
    const firstTrigger = data.triggers[0];
    if (firstTrigger?.kind === "webhook") {
      setTriggerKind("webhook");
      setEventFiltersText(stringifyEventFilters(firstTrigger.event_filters));
    } else {
      setTriggerKind("schedule");
      if (firstTrigger?.cron_expression) {
        setTriggerConfig(parseCronExpression(firstTrigger.cron_expression, firstTrigger.timezone));
      }
    }
  }, [autopilotId, data]);

  const assigneeItems = useMemo(() => {
    const agentItems: Array<PickerItem<string> & AssigneeSelection> = agents
      .filter((agent) => !agent.archived_at)
      .map((agent) => ({
        detail: agent.description || t("autopilots.agent"),
        id: agent.id,
        label: agent.name,
        type: "agent" as const,
        value: `agent:${agent.id}`,
      }));
    const squadItems: Array<PickerItem<string> & AssigneeSelection> = squads
      .filter((squad) => !squad.archived_at)
      .map((squad) => ({
        detail: squad.description || t("autopilots.squad"),
        id: squad.id,
        label: squad.name,
        type: "squad" as const,
        value: `squad:${squad.id}`,
      }));
    return [...agentItems, ...squadItems];
  }, [agents, squads, t]);

  const selectedAssigneeName = assignee
    ? getActorName(assignee.type, assignee.id, agents, squads)
    : t("autopilots.select_assignee");
  const selectedProjectTitle = getProjectTitle(projectId, projects);
  const firstTrigger = data?.triggers[0] ?? null;
  const triggerKindLocked = isEdit && firstTrigger ? firstTrigger.kind !== triggerKind : false;
  const canSubmit =
    title.trim().length > 0 &&
    Boolean(assignee) &&
    (triggerKind === "webhook" ||
      (triggerConfig.frequency === "custom"
        ? toCronExpression(triggerConfig).length > 0
        : true));
  const submitting =
    createAutopilot.isPending ||
    updateAutopilot.isPending ||
    createTrigger.isPending ||
    updateTrigger.isPending;
  const bottomContentPadding = keyboardFrame.height > 0
    ? keyboardFrame.height + Math.max(insets.bottom, spacing.lg)
    : Math.max(insets.bottom, spacing.lg);

  useEffect(() => {
    keyboardFrameRef.current = keyboardFrame;
  }, [keyboardFrame]);

  const scrollInputIntoView = useCallback((inputRef: RefObject<TextInput | null>) => {
    const input = inputRef.current;
    if (!input) return;

    input.measureInWindow((_x, y, _width, height) => {
      const frame = keyboardFrameRef.current;
      const visibleBottom = frame.screenY - KEYBOARD_SCROLL_GAP;
      const inputBottom = y + height;
      const overlap = inputBottom - visibleBottom;
      if (overlap <= 0) return;

      scrollViewRef.current?.scrollTo({
        animated: true,
        y: Math.max(0, scrollYRef.current + overlap + KEYBOARD_SCROLL_GAP),
      });
    });
  }, []);

  const handleInputFocus = useCallback((inputRef: RefObject<TextInput | null>) => {
    activeInputRef.current = inputRef;
    setTimeout(() => {
      scrollInputIntoView(inputRef);
    }, FOCUS_SCROLL_DELAY_MS);
  }, [scrollInputIntoView]);

  useEffect(() => {
    if (keyboardFrame.height <= 0 || !activeInputRef.current) return;
    const inputRef = activeInputRef.current;
    const timer = setTimeout(() => {
      scrollInputIntoView(inputRef);
    }, spacing.sm * 10);
    return () => clearTimeout(timer);
  }, [keyboardFrame.height, keyboardFrame.screenY, scrollInputIntoView]);

  if (isEdit && isLoading) return <LoadingState />;

  async function handleSave() {
    if (!canSubmit || !assignee) return;
    const base = {
      title: title.trim(),
      description: description.trim() || undefined,
      project_id: executionMode === "create_issue" ? projectId : null,
      assignee_type: assignee.type,
      assignee_id: assignee.id,
      execution_mode: executionMode,
    };

    try {
      if (!isEdit) {
        const autopilot = await createAutopilot.mutateAsync(base);
        if (triggerKind === "webhook") {
          const trigger = await createTrigger.mutateAsync({
            autopilotId: autopilot.id,
            kind: "webhook",
            event_filters: parseEventFilters(eventFiltersText),
          });
          setCreatedWebhookTrigger(trigger);
          return;
        }
        await createTrigger.mutateAsync({
          autopilotId: autopilot.id,
          kind: "schedule",
          cron_expression: toCronExpression(triggerConfig),
          timezone: triggerConfig.timezone,
        });
        navigation.replace("AutopilotDetail", { autopilotId: autopilot.id });
        return;
      }

      await updateAutopilot.mutateAsync({
        id: autopilotId!,
        ...base,
        description: description.trim() || null,
      });

      if (firstTrigger) {
        if (triggerKind === "schedule" && firstTrigger.kind === "schedule") {
          await updateTrigger.mutateAsync({
            autopilotId: autopilotId!,
            triggerId: firstTrigger.id,
            cron_expression: toCronExpression(triggerConfig),
            timezone: triggerConfig.timezone,
          });
        }
        if (triggerKind === "webhook" && firstTrigger.kind === "webhook") {
          await updateTrigger.mutateAsync({
            autopilotId: autopilotId!,
            triggerId: firstTrigger.id,
            event_filters: parseEventFilters(eventFiltersText),
          });
        }
      } else if (triggerKind === "schedule") {
        await createTrigger.mutateAsync({
          autopilotId: autopilotId!,
          kind: "schedule",
          cron_expression: toCronExpression(triggerConfig),
          timezone: triggerConfig.timezone,
        });
      } else {
        await createTrigger.mutateAsync({
          autopilotId: autopilotId!,
          kind: "webhook",
          event_filters: parseEventFilters(eventFiltersText),
        });
      }

      navigation.goBack();
    } catch (err) {
      const message = err instanceof Error && err.message ? err.message : t("autopilots.save_failed");
      Alert.alert(t("autopilots.save_failed"), message);
    }
  }

  if (createdWebhookTrigger) {
    const webhookUrl = buildAutopilotWebhookUrl({
      trigger: createdWebhookTrigger,
      apiBaseUrl: api.getBaseUrl(),
    }) ?? "";
    return (
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar
          onBack={() => navigation.replace("AutopilotDetail", { autopilotId: createdWebhookTrigger.autopilot_id })}
          title={t("autopilots.webhook_created")}
        />
        <View style={styles.createdWrap}>
          <View style={styles.createdIcon}>
            <Webhook color={colors.foreground} size={28} />
          </View>
          <Text style={styles.createdTitle}>{t("autopilots.webhook_created")}</Text>
          <Text style={styles.createdDetail}>{t("autopilots.webhook_created_detail")}</Text>
          <View style={styles.urlBox}>
            <Text selectable style={styles.urlText}>
              {webhookUrl}
            </Text>
          </View>
          <Button
            onPress={() => {
              Clipboard.setString(webhookUrl);
            }}
          >
            {t("autopilots.copy_url")}
          </Button>
          <Button
            onPress={() => navigation.replace("AutopilotDetail", { autopilotId: createdWebhookTrigger.autopilot_id })}
            variant="secondary"
          >
            {t("common.done")}
          </Button>
        </View>
      </Screen>
    );
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        title={isEdit ? t("autopilots.edit") : t("autopilots.create")}
      />
      <View style={styles.keyboard}>
        <ScrollView
          contentContainerStyle={[
            styles.content,
            { paddingBottom: bottomContentPadding },
          ]}
          keyboardDismissMode={Platform.OS === "ios" ? "interactive" : undefined}
          keyboardShouldPersistTaps="handled"
          onScroll={(event) => {
            scrollYRef.current = event.nativeEvent.contentOffset.y;
          }}
          ref={scrollViewRef}
          scrollEventThrottle={16}
        >
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{t("autopilots.runbook")}</Text>
            <Field
              inputRef={titleInputRef}
              label={t("autopilots.name")}
              onChangeText={setTitle}
              onFocus={() => handleInputFocus(titleInputRef)}
              value={title}
            />
            <Field
              inputRef={promptInputRef}
              label={t("autopilots.prompt")}
              multiline
              onChangeText={setDescription}
              onFocus={() => handleInputFocus(promptInputRef)}
              value={description}
            />
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{t("autopilots.configuration")}</Text>
            <PickerButton
              label={t("autopilots.assignee")}
              onPress={() => setAssigneePickerOpen(true)}
              value={selectedAssigneeName}
            />
            <Segment
              options={[
                { label: t("autopilots.create_issue"), value: "create_issue" },
                { label: t("autopilots.run_only"), value: "run_only" },
              ]}
              value={executionMode}
              onChange={setExecutionMode}
            />
            {executionMode === "create_issue" ? (
              <PickerButton
                label={t("autopilots.project")}
                onPress={() => setProjectPickerOpen(true)}
                value={selectedProjectTitle}
              />
            ) : null}
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{t("autopilots.trigger")}</Text>
            <Segment
              disabled={triggerKindLocked}
              options={[
                { label: t("autopilots.schedule"), value: "schedule" },
                { label: t("autopilots.webhook"), value: "webhook" },
              ]}
              value={triggerKind}
              onChange={setTriggerKind}
            />
            {triggerKindLocked ? (
              <Text style={styles.helpText}>{t("autopilots.trigger_kind_locked")}</Text>
            ) : null}
            {triggerKind === "schedule" ? (
              <AutopilotScheduleFields
                config={triggerConfig}
                cronInputRef={cronInputRef}
                onChange={setTriggerConfig}
                onCronFocus={() => handleInputFocus(cronInputRef)}
              />
            ) : (
              <View style={styles.formGap}>
                <View style={styles.inlineHeader}>
                  <Text style={styles.helpText}>{t("autopilots.webhook_hint")}</Text>
                  <Pressable
                    accessibilityRole="button"
                    onPress={() => {
                      navigation.navigate("ExternalWeb", {
                        title: t("autopilots.event_filter_docs"),
                        url: AUTOPILOT_EVENT_FILTER_DOC_URL,
                      });
                    }}
                    style={({ pressed }) => [styles.helpButton, pressed && styles.pressed]}
                  >
                    <CircleHelp color={colors.foreground} size={15} />
                    <Text style={styles.helpButtonText}>{t("autopilots.event_filter_docs")}</Text>
                  </Pressable>
                </View>
                <Field
                  inputRef={eventFiltersInputRef}
                  label={t("autopilots.event_filters")}
                  multiline
                  onChangeText={setEventFiltersText}
                  onFocus={() => handleInputFocus(eventFiltersInputRef)}
                  value={eventFiltersText}
                />
                <Text style={styles.helpText}>{t("autopilots.event_filters_hint")}</Text>
              </View>
            )}
          </View>

          <Button disabled={!canSubmit || submitting} onPress={() => void handleSave()}>
            {submitting
              ? t("autopilots.saving")
              : isEdit
                ? t("common.save")
                : t("autopilots.create")}
          </Button>
        </ScrollView>
      </View>

      <SelectionModal
        items={assigneeItems}
        onClose={() => setAssigneePickerOpen(false)}
        onSelect={(item) => {
          setAssignee({ id: item.id, type: item.type });
          setAssigneePickerOpen(false);
        }}
        open={assigneePickerOpen}
        title={t("autopilots.select_assignee")}
      />
      <SelectionModal
        items={[
          { label: t("autopilots.no_project"), value: "" },
          ...projects.map((project) => ({
            detail: project.description ?? undefined,
            label: project.title,
            value: project.id,
          })),
        ]}
        onClose={() => setProjectPickerOpen(false)}
        onSelect={(item) => {
          setProjectId(item.value || null);
          setProjectPickerOpen(false);
        }}
        open={projectPickerOpen}
        title={t("autopilots.project")}
      />
    </Screen>
  );
}

function Field({
  inputRef,
  label,
  multiline,
  onChangeText,
  onFocus,
  value,
}: {
  inputRef?: RefObject<TextInput | null>;
  label: string;
  multiline?: boolean;
  onChangeText: (value: string) => void;
  onFocus?: () => void;
  value: string;
}) {
  return (
    <View style={styles.fieldWrap}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        multiline={multiline}
        onChangeText={onChangeText}
        onFocus={onFocus}
        placeholderTextColor={colors.mutedForeground}
        ref={inputRef}
        style={[styles.field, multiline && styles.multilineField]}
        value={value}
      />
    </View>
  );
}

function PickerButton({
  label,
  onPress,
  value,
}: {
  label: string;
  onPress: () => void;
  value: string;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.pickerButton, pressed && styles.pressed]}
    >
      <View style={styles.pickerText}>
        <Text style={styles.fieldLabel}>{label}</Text>
        <Text numberOfLines={1} style={styles.pickerValue}>
          {value}
        </Text>
      </View>
      <ChevronRight color={colors.mutedForeground} size={18} />
    </Pressable>
  );
}

function Segment<T extends string>({
  disabled,
  onChange,
  options,
  value,
}: {
  disabled?: boolean;
  onChange: (value: T) => void;
  options: Array<{ label: string; value: T }>;
  value: T;
}) {
  return (
    <View style={[styles.segment, disabled && styles.disabled]}>
      {options.map((option) => (
        <Pressable
          accessibilityRole="button"
          disabled={disabled}
          key={option.value}
          onPress={() => onChange(option.value)}
          style={[
            styles.segmentItem,
            option.value === value ? styles.segmentItemActive : null,
          ]}
        >
          <Text
            numberOfLines={1}
            style={[
              styles.segmentText,
              option.value === value ? styles.segmentTextActive : null,
            ]}
          >
            {option.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

function SelectionModal<T extends PickerItem<string>>({
  items,
  onClose,
  onSelect,
  open,
  title,
}: {
  items: T[];
  onClose: () => void;
  onSelect: (item: T) => void;
  open: boolean;
  title: string;
}) {
  const { t } = useTranslation();
  return (
    <Modal animationType="slide" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.modalBackdrop}>
        <View style={styles.modalSheet}>
          <Text style={styles.modalTitle}>{title}</Text>
          <ScrollView contentContainerStyle={styles.modalList}>
            {items.map((item) => (
              <Pressable
                accessibilityRole="button"
                key={item.value}
                onPress={() => onSelect(item)}
                style={({ pressed }) => [styles.optionRow, pressed && styles.pressed]}
              >
                <View style={styles.optionIcon}>
                  {item.icon ?? <Rocket color={colors.foreground} size={16} />}
                </View>
                <View style={styles.optionText}>
                  <Text numberOfLines={1} style={styles.optionLabel}>
                    {item.label}
                  </Text>
                  {item.detail ? (
                    <Text numberOfLines={2} style={styles.optionDetail}>
                      {item.detail}
                    </Text>
                  ) : null}
                </View>
                <Check color={colors.mutedForeground} size={16} />
              </Pressable>
            ))}
          </ScrollView>
          <Button onPress={onClose} variant="secondary">
            {t("common.close")}
          </Button>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  keyboard: {
    flex: 1,
  },
  content: {
    gap: spacing.md,
    padding: spacing.lg,
  },
  section: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    padding: spacing.md,
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  formGap: {
    gap: spacing.md,
  },
  fieldWrap: {
    gap: spacing.xs,
  },
  fieldLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  field: {
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 15,
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  multilineField: {
    minHeight: 140,
    textAlignVertical: "top",
  },
  pickerButton: {
    alignItems: "center",
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 52,
    paddingHorizontal: spacing.md,
  },
  pickerText: {
    flex: 1,
    gap: 2,
    minWidth: 0,
  },
  pickerValue: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  pressed: {
    opacity: 0.72,
  },
  segment: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.xs,
    padding: spacing.xs,
  },
  segmentItem: {
    alignItems: "center",
    borderRadius: radii.sm,
    flexGrow: 1,
    justifyContent: "center",
    minHeight: 36,
    minWidth: 92,
    paddingHorizontal: spacing.sm,
  },
  segmentItemActive: {
    backgroundColor: colors.card,
  },
  segmentText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  segmentTextActive: {
    color: colors.foreground,
  },
  disabled: {
    opacity: 0.55,
  },
  helpText: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 17,
  },
  inlineHeader: {
    gap: spacing.sm,
  },
  helpButton: {
    alignItems: "center",
    alignSelf: "flex-start",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 34,
    paddingHorizontal: spacing.sm,
  },
  helpButtonText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
  },
  modalBackdrop: {
    backgroundColor: "rgba(24,24,27,0.35)",
    flex: 1,
    justifyContent: "flex-end",
  },
  modalSheet: {
    backgroundColor: colors.background,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    gap: spacing.md,
    maxHeight: "86%",
    padding: spacing.lg,
  },
  modalTitle: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
  },
  modalList: {
    gap: spacing.sm,
  },
  optionRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    padding: spacing.md,
  },
  optionIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 34,
    justifyContent: "center",
    width: 34,
  },
  optionText: {
    flex: 1,
    minWidth: 0,
  },
  optionLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  optionDetail: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 17,
    marginTop: 2,
  },
  createdWrap: {
    alignItems: "center",
    flex: 1,
    gap: spacing.md,
    justifyContent: "center",
    padding: spacing.xl,
  },
  createdIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: 24,
    height: 56,
    justifyContent: "center",
    width: 56,
  },
  createdTitle: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "600",
    textAlign: "center",
  },
  createdDetail: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
    textAlign: "center",
  },
  urlBox: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.md,
    width: "100%",
  },
  urlText: {
    color: colors.foreground,
    fontSize: 12,
    lineHeight: 18,
  },
});
