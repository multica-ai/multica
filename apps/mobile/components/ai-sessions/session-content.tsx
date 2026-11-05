import { deliverSubmission } from "@/data/ai-sessions/submission-delivery";
import { submissionPresentation,submissionStopTarget } from "@/data/ai-sessions/submission-presentation";
import {
  submissionOutbox,
  useSubmissionOutbox,
  EMPTY_SUBMISSIONS,
  type MobileSubmission,
} from "@/data/ai-sessions/submission-outbox";
import { invalidateAISessionLists } from "@/data/ai-sessions/list-queries";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActionSheetIOS,
  ActivityIndicator,
  Alert,
  Keyboard,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  Stack,
  router,
  useFocusEffect,
  useLocalSearchParams,
} from "expo-router";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import * as DocumentPicker from "expo-document-picker";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { SessionMarkdown } from "@/components/ai-sessions/session-markdown";
import { useT } from "@/lib/i18n";
import { createRequestId } from "@/lib/request-id";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";
import { cn } from "@/lib/utils";
import { continuousCorners } from "@/lib/radius";
import { aiKeys } from "@/data/ai-sessions/client";
import {
  useAIConnection,
  useAISessionScope,
  useAISubscriptionRejected,
} from "@/data/ai-sessions/hooks";
import { draftUploads } from "@/data/ai-sessions/uploads";
import { useStoredSnapshot } from "@/data/ai-sessions/snapshot-store";
import {
  useAIDrafts,
  aiDraftKey,
  EMPTY_DRAFT_ATTACHMENTS,
  type DraftAttachment,
} from "@/data/ai-sessions/drafts";
import {
  EMPTY_REFERENCES,
  ReferenceUnavailableError,
  referenceKey,
} from "@/data/ai-sessions/references";
import { resolveAIMessage } from "@/data/ai-sessions/reference-queries";
import { aiSubmission,canReplaySubmission } from "@/data/ai-sessions/submission";
import {
  ReferenceChips,
  AIMessageContent,
} from "@/components/ai-sessions/reference-chips";
import { tokenAtCursor } from "@/lib/mention-serialize";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  commandResultSchema,
  historySchema,
  providersSchema,
  queueItemSchema,
  queueSchema,
} from "@/data/ai-sessions/schemas";
import {
  groupTranscriptActivities,
  transcript,
  type ConversationRow,
} from "@/data/ai-sessions/projection";
import { AIActivityGroup } from "@/components/ai-sessions/activity-group";
import { ConnectionNotice } from "@/components/ai-sessions/connection-notice";
import { SessionAttachment } from "@/components/ai-sessions/session-attachment";
import { openDeviceFile, remoteWebURL } from "@/data/ai-sessions/files";
import { SessionQuestions } from "@/components/ai-sessions/questions";

export function AISessionContent({
  sessionId,
  embedded = false,
  active = true,
  onOpenSource,
}: {
  sessionId?: string;
  embedded?: boolean;
  active?: boolean;
  onOpenSource?: () => void;
}) {
  const { t } = useT("aiSessions");
  const showError=useCallback(()=>Alert.alert(t("error_title"),t("operation_failed")),[t]);
  const {
    workspace,
    device,
    session: routeSession,
    name,
    turn,
  } = useLocalSearchParams<{
    workspace: string;
    device: string;
    session: string;
    name?: string;
    turn?: string;
  }>();
  const session = sessionId ?? routeSession;
  const [focused, setFocused] = useState(false);
  useFocusEffect(
    useCallback(() => {
      setFocused(true);
      return () => setFocused(false);
    }, []),
  );
  const scope = useAISessionScope();
  const { client, connected } = useAIConnection();
  const subscriptionRejected = useAISubscriptionRejected(session);
  const qc = useQueryClient();
  const key = aiKeys.snapshot(scope, session);
  // The conversation as last read shows at once; the daemon sends what changed since.
  const restored = useStoredSnapshot(scope, session, key);
  const query = useQuery({
    queryKey: key,
    enabled: !!client && restored,
    retry: false,
    staleTime: Infinity,
    queryFn: ({ signal }) => client!.snapshot(session, signal),
  });
  const snapshot = query.data;
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const draftKey = aiDraftKey(scope, session);
  const outboxKey = JSON.stringify([
    process.env.EXPO_PUBLIC_API_URL,
    scope.userId,
    scope.workspaceId,
    scope.deviceId,
    session,
  ]);
  const outbox = useSubmissionOutbox(
    (state) => state.records[outboxKey] ?? EMPTY_SUBMISSIONS,
  );
  const recovering = useRef(false);
  const draft = useAIDrafts((s) => s.drafts[draftKey] ?? "");
  const setDraft = useAIDrafts((s) => s.setDraft);
  const pending = useAIDrafts((s) => s.pending[draftKey]);
  const takeOwnership=useAIDrafts((s)=>s.takeOwnership);
  const references = useAIDrafts(
    (s) => s.references[draftKey] ?? EMPTY_REFERENCES,
  );
  const setReferences = useAIDrafts((s) => s.setReferences);
  const complete = useAIDrafts((s) => s.complete);
  const agents = references.filter((entry) => entry.kind === "agent");
  const input = useRef<TextInput>(null);
  const selection = useRef({ start: 0, end: 0 });
  const returningFromPicker = useRef<{
    textLength: number;
    cursor: number;
  } | null>(null);
  const submitting = useRef(false);
  const attachments = useAIDrafts(
    (state) => state.attachments[draftKey] ?? EMPTY_DRAFT_ATTACHMENTS,
  );
  const updateAttachments = useAIDrafts((state) => state.setAttachments);
  const setAttachments = (
    value:
      | DraftAttachment[]
      | ((files: DraftAttachment[]) => DraftAttachment[]),
  ) => updateAttachments(draftKey, value);
  const uploading = attachments.some((file) => file.status === "uploading");
  const [busy, setBusy] = useState(false);
  const [sendFailed, setSendFailed] = useState(false);
  const [referenceError, setReferenceError] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const list = useRef<FlashListRef<ConversationRow>>(null);
  const nearBottom = useRef(true);
  const rows = useMemo(
    () => (snapshot ? transcript(snapshot) : []),
    [snapshot],
  );
  const history = useInfiniteQuery({
    queryKey: [
      ...aiKeys.scope(scope),
      "history",
      session,
      snapshot?.store_generation,
      snapshot?.turns[0]?.turn_index,
    ],
    enabled: false,
    initialPageParam: snapshot?.turns[0]?.turn_index ?? 0,
    queryFn: ({ signal, pageParam }) =>
      client!.request(
        "GET",
        `/sessions/${encodeURIComponent(session)}/history?before_index=${pageParam}`,
        historySchema,
        undefined,
        signal,
      ),
    getNextPageParam: (page) =>
      page.has_older_turns && page.turns.length
        ? page.turns[0].turn_index
        : undefined,
  });
  const historyTurns = useMemo(
    () =>
      history.data?.pages.flatMap((page) =>
        page.store_generation === snapshot?.store_generation ? page.turns : [],
      ) ?? [],
    [history.data, snapshot?.store_generation],
  );
  const allRows = useMemo(
    () =>
      snapshot
        ? groupTranscriptActivities(
            transcript({
              ...snapshot,
              turns: [
                ...new Map(
                  [...historyTurns, ...snapshot.turns].map((turn) => [
                    turn.id,
                    turn,
                  ]),
                ).values(),
              ].sort((a, b) => a.turn_index - b.turn_index),
            }),
          )
        : [],
    [snapshot, historyTurns],
  );
  const hasOlder = history.data
    ? history.hasNextPage
    : snapshot?.has_older_turns;
  const [readingHistory, setReadingHistory] = useState(false);
  const stopTarget=submissionStopTarget(outbox,snapshot,client?.submissionLifecycle===true);
  const running=stopTarget!==null;
  const submissionView = submissionPresentation(outbox, snapshot);
  const queuedCount = Math.max(
    snapshot?.queue.items.filter((item) => {
      const state = snapshot.submissions.find(
        (value) => value.request_id === item.requestId,
      )?.state;
      return (
        state !== "starting" && state !== "attached" && state !== "removed"
      );
    }).length ?? 0,
    submissionView.queued.length,
  );
  const hasMessage = !!(
    draft.trim() ||
    attachments.length ||
    references.length
  );
  const showStop = running && !hasMessage;
  useEffect(() => {
    if (restored && connected && focused && active) client?.select(session);
  }, [restored, client, connected, session, focused, active]);
  useEffect(
    () => (client && focused && active ? client.focus(session) : undefined),
    [client, session, focused, active],
  );
  useFocusEffect(
    useCallback(() => {
      const restore = returningFromPicker.current;
      if (!restore || !active) return;
      returningFromPicker.current = null;
      const timer = setTimeout(() => {
        const value = useAIDrafts.getState().drafts[draftKey] ?? "";
        const cursor = Math.max(
          0,
          Math.min(
            value.length,
            restore.cursor + value.length - restore.textLength,
          ),
        );
        input.current?.focus();
        input.current?.setNativeProps({
          selection: { start: cursor, end: cursor },
        });
      }, 200);
      return () => clearTimeout(timer);
    }, [active, draftKey]),
  );
  useEffect(() => {
    if (nearBottom.current && rows.length)
      list.current?.scrollToEnd({ animated: false });
  }, [rows]);
  useEffect(() => {
    if (snapshot) client?.committed(session);
  }, [client, session, snapshot]);
  useEffect(() => {
    void submissionOutbox.load(outboxKey).catch(showError);
  }, [outboxKey,showError]);
  useEffect(() => {
    if (!client || !connected || !snapshot || recovering.current) return;
    recovering.current = true;
    void (async () => {
      await submissionOutbox.load(outboxKey);
      const legacy = useAIDrafts.getState().pending[draftKey];
      if (legacy) {
        const request = {
          ...legacy,
          createdAt: legacy.createdAt ?? Date.now(),
        };
        await submissionOutbox.put(outboxKey, {
          request,
          body: aiSubmission({ ...request, queued: true }),
          generation: "",
          lifecycle: false,
          confirmed: false,
        });
        complete(draftKey, request);
      }
      const records =
        useSubmissionOutbox.getState().records[outboxKey] ?? EMPTY_SUBMISSIONS;
      const settled = new Set<string>();
      for (const record of records) {
        const authority = snapshot.submissions.find(
          (value) => value.request_id === record.request.requestId,
        );
        const nativeId =
          authority?.attempt?.request_id ?? record.request.requestId;
        if (
          authority?.state === "removed" || authority?.state==="attached" ||
          (!client.submissionLifecycle&&snapshot.turns.some((value) => value.request_id === nativeId))
        ) {
          draftUploads.forget(record.assets?.map((asset)=>asset.id)??record.request.attachments);
          await submissionOutbox.remove(outboxKey, record.request.requestId);
          settled.add(record.request.requestId);
          continue;
        }
        if (authority && authority.revision !== record.authority?.revision)
          await submissionOutbox.confirm(
            outboxKey,
            record.request.requestId,
            authority,
          );
        if (record.confirmed&&authority) continue;
        if(!client.submissionLifecycle||!record.lifecycle){
          const result=await client.promptStatuses(session,[record.request.requestId]);
          if(result.statuses[record.request.requestId]==="delivered"||result.statuses[record.request.requestId]==="removed"){
            draftUploads.forget(record.assets?.map((asset)=>asset.id)??record.request.attachments);
            await submissionOutbox.remove(outboxKey,record.request.requestId);settled.add(record.request.requestId);
          }
          continue;
        }
        if(record.generation!==client.storeGeneration)continue;
        const state = await client.submissionStatuses(session, [
          record.request.requestId,
        ]);
        const value = state.submissions.find(
          (value) => value.request_id === record.request.requestId,
        );
        if (value) {
          if (value.state === "attached" || value.state === "removed") {
            draftUploads.forget(record.assets?.map((asset)=>asset.id)??record.request.attachments);
          await submissionOutbox.remove(outboxKey, record.request.requestId);
            settled.add(record.request.requestId);
          } else
            await submissionOutbox.confirm(
              outboxKey,
              record.request.requestId,
              value,
            );
        } else if (
          canReplaySubmission(record,state)
        ) {
          await deliverSubmission(client,session,outboxKey,draftKey,record,()=>{
            if(useAuthStore.getState().user?.id!==scope.userId||useWorkspaceStore.getState().currentWorkspaceId!==scope.workspaceId)throw new Error("scope_changed");
          });

        }
      }
      if (client.submissionLifecycle) {
        const mirror: MobileSubmission[] = snapshot.queue.items.flatMap(
          (item,position) => {
            const parsed = queueItemSchema.safeParse(item);
            if (!parsed.success || settled.has(parsed.data.requestId))
              return [];
            const request = {
              requestId: parsed.data.requestId,
              text: parsed.data.content,
              attachments: parsed.data.attachmentIds,
              queued: true,
              queuePaused: snapshot.queue.paused,
              createdAt: Date.now(),
            };
            return [
              {
                request,
                body: { type: "submit", item },
                generation: snapshot.store_generation,
                lifecycle: true,
                confirmed: true,
                queueRevision:snapshot.queue.revision,position,
                authority: snapshot.submissions.find(
                  (value) => value.request_id === request.requestId,
                ),
              },
            ];
          },
        );
        if (
          mirror.some(
            (item) =>
              !records.some(
                (old) => old.request.requestId === item.request.requestId&&old.queueRevision===item.queueRevision&&old.position===item.position&&old.request.queuePaused===item.request.queuePaused,
              ),
          )
        )
          await submissionOutbox.mirror(outboxKey, mirror);
      }
    })()
      .catch(() => setSendFailed(true))
      .finally(() => {
        recovering.current = false;
      });
  }, [client, connected, snapshot, outboxKey, draftKey, complete,session,pending?.requestId,scope.userId,scope.workspaceId]);
  const openReferences = (
    token?: { start: number; query: string },
    cursor = selection.current.start,
  ) => {
    if (
      !connected ||
      busy ||
      pending ||
      snapshot?.conversation.archived ||
      !active ||
      returningFromPicker.current
    )
      return;
    returningFromPicker.current = {
      textLength: (useAIDrafts.getState().drafts[draftKey] ?? "").length,
      cursor,
    };
    Keyboard.dismiss();
    router.push({
      pathname: "/[workspace]/ai-sessions/[device]/references",
      params: {
        workspace,
        device,
        session,
        name,
        ...(token
          ? {
              query: token.query,
              tokenStart: String(token.start),
              tokenEnd: String(cursor),
            }
          : {}),
      },
    });
  };
  const changeDraft = (value: string) => {
    const cursor = Math.max(
      0,
      Math.min(
        value.length,
        selection.current.start + value.length - draft.length,
      ),
    );
    setDraft(draftKey, value);
    setReferenceError("");
    if (value[cursor - 1] !== "@") return;
    const token = tokenAtCursor(value, cursor);
    if (token) openReferences(token, cursor);
  };

  const stop = async () => {
    if (!client || !connected || !running || busy) return;
    setBusy(true);
    try {
      if(client.submissionLifecycle)await client.command(session,{type:"queue_update",action:"stop",request_id:stopTarget?.requestId,turn_id:stopTarget?.turnId},queueSchema);
      else await client.command(session, { type: "interrupt" }, commandResultSchema);
      await query.refetch();
    } catch {
      showError();
    } finally {
      setBusy(false);
    }
  };
  const chooseProvider = async () => {
    if (!client || !connected || running || busy || uploading || pending)
      return;
    const available = (
      await client.request("GET", "/providers", providersSchema)
    ).providers.filter((p) => p.available);
    ActionSheetIOS.showActionSheetWithOptions(
      {
        title: t("provider"),
        options: [t("cancel"), ...available.map((p) => p.name)],
        cancelButtonIndex: 0,
      },
      (index) => {
        const provider = available[index - 1];
        if (!provider || provider.id === snapshot?.conversation.provider)
          return;
        setBusy(true);
        void client
          .request("POST", "/sessions", localV2SummarySchema, {
            id: `ios-${createRequestId()}`,
            provider: provider.id,
            title: t("new"),
            directory_source_id: session,
          })
          .then((created) => {
            setDraft(aiDraftKey(scope, created.conversation_id), draft);
            setReferences(
              aiDraftKey(scope, created.conversation_id),
              references,
            );
            updateAttachments(
              aiDraftKey(scope, created.conversation_id),
              attachments,
            );
            void invalidateAISessionLists(qc, scope);
            router.replace({
              pathname: "/[workspace]/ai-sessions/[device]/[session]",
              params: {
                workspace,
                device,
                session: created.conversation_id,
                name,
              },
            });
          })
          .catch(showError)
          .finally(() => setBusy(false));
      },
    );
  };
  const transmit=async(record:MobileSubmission)=>{
    if(!client)return;
    await deliverSubmission(client,session,outboxKey,draftKey,record,()=>{
      if(useAuthStore.getState().user?.id!==scope.userId||useWorkspaceStore.getState().currentWorkspaceId!==scope.workspaceId)throw new Error("scope_changed");
    });
    void query.refetch();
  };
  const send = async () => {
    if (
      !client ||
      !connected ||
      !snapshot ||
      busy ||
      submitting.current ||
      snapshot.conversation.archived ||
      !hasMessage
    )
      return;
    submitting.current = true;
    setBusy(true);
    setSendFailed(false);
    setReferenceError("");
    try {
      const prepared = await resolveAIMessage(client, draft, references);
      if (
        useAuthStore.getState().user?.id !== scope.userId ||
        useWorkspaceStore.getState().currentWorkspaceId !== scope.workspaceId
      )
        throw new Error("scope_changed");
      const assets = await draftUploads.prepare(
        attachments.map((file) => file.id),
      );
      const stored = attachments.map((file) => file.id);
      const request = {
        requestId: createRequestId(),
        text: draft,
        attachments: stored,
        prepared,
        queued:
          running ||
          snapshot.queue.items.length > 0 ||
          snapshot.queue.paused ||
          outbox.some((value) => !value.confirmed),
        queuePaused: snapshot.queue.paused,
        createdAt: Date.now(),
      };
      const body = aiSubmission(
        { ...request, queued: true },
        client.submissionLifecycle,
        snapshot.settings,
      );
      const record: MobileSubmission = {
        request,
        body,
        assets,
        generation: client.storeGeneration,
        lifecycle: client.submissionLifecycle,
        confirmed: false,
      };
      await submissionOutbox.put(outboxKey, record);
      // Only a committed local record may take ownership of the draft.
      takeOwnership(draftKey,request);
      client.pendingShown(session, request.requestId);
      submitting.current = false;
      setBusy(false);
      void transmit(record).catch(() => setSendFailed(true));
    } catch (error) {
      setReferenceError(
        error instanceof ReferenceUnavailableError
          ? t("references.unavailable", { name: error.reference.name })
          : t("send_failed"),
      );
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };
  const upload = async () => {
    if (!client || !connected || busy || pending) return;
    const result = await DocumentPicker.getDocumentAsync({
      multiple: true,
      copyToCacheDirectory: true,
    });
    if (result.canceled) return;
    for (const asset of result.assets)
      draftUploads.start(client, draftKey, {
        uri: asset.uri,
        name: asset.name,
        mimeType: asset.mimeType,
      });
  };
  const removeAttachment = (id: string) => {
    setAttachments((files) => files.filter((file) => file.id !== id));
    if (client) draftUploads.remove(client, id);
  };
  const metadata = (body: Record<string, unknown>) =>
    client!
      .command(session, { type: "update", ...body }, localV2SummarySchema)
      .then(() => {
        void query.refetch();
        void invalidateAISessionLists(qc, scope);
      });
  const actions = () => {
    if (!snapshot) return;
    const choices = [
      {
        label: t("rename"),
        run: () =>
          Alert.prompt(
            t("rename"),
            undefined,
            [
              { text: t("cancel"), style: "cancel" },
              {
                text: t("save"),
                onPress: (value?: string) => {
                  if (value?.trim())
                    void metadata({ title: value.trim() }).catch(showError);
                },
              },
            ],
            "plain-text",
            snapshot.conversation.title,
          ),
      },
      {
        label: t(snapshot.conversation.pinned ? "unpin" : "pin"),
        run: () => {
          void metadata({ pinned: !snapshot.conversation.pinned }).catch(
            showError,
          );
        },
      },
      {
        label: t("usage"),
        run: () =>
          router.push({
            pathname: "/[workspace]/ai-sessions/[device]/usage",
            params: { workspace, device, session, name },
          }),
      },
      ...(!running && !snapshot.conversation.archived && !pending
        ? [
            {
              label: t("new_with_provider"),
              run: () => {
                void chooseProvider().catch(showError);
              },
            },
          ]
        : []),
      {
        label: t(snapshot.conversation.archived ? "unarchive" : "archive"),
        run: () => {
          void metadata({ archived: !snapshot.conversation.archived }).catch(
            showError,
          );
        },
      },
    ];
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: [
          t("cancel"),
          ...choices.map((choice) => String(choice.label)),
        ],
        cancelButtonIndex: 0,
      },
      (index) => choices[index - 1]?.run(),
    );
  };
  const located = useRef("");
  useEffect(() => {
    if (!focused) {
      located.current = "";
      return;
    }
    if (!turn || located.current === turn || !active) return;
    const index = allRows.findIndex((row) => row.turnId === turn);
    if (index >= 0) {
      located.current = turn;
      nearBottom.current = false;
      list.current?.scrollToIndex({ index, animated: true });
    } else if (hasOlder && !history.isFetching && connected)
      void history.fetchNextPage();
  }, [turn, allRows, focused, active, hasOlder, history, connected]);
  const sideRequest = useRef<{ key: string; requestId: string } | null>(null);
  const createSide = async (quote: string, turnId?: string) => {
    if (!client || !connected || busy || !quote.trim()) return;
    const boundary = JSON.stringify([session, turnId, quote]);
    if (sideRequest.current?.key !== boundary)
      sideRequest.current = { key: boundary, requestId: createRequestId() };
    setBusy(true);
    try {
      const created = await client.command(
        session,
        {
          type: "create_side",
          quote,
          turn_id: turnId,
          request_id: sideRequest.current.requestId,
        },
        localV2SummarySchema,
      );
      sideRequest.current = null;
      void invalidateAISessionLists(qc, scope);
      router.push({
        pathname: "/[workspace]/ai-sessions/[device]/side",
        params: {
          workspace,
          device,
          session,
          sideId: created.conversation_id,
          tab: "side",
          name,
        },
      });
    } catch {
      showError();
    } finally {
      setBusy(false);
    }
  };
  const resource = (value: string): boolean => {
    if (/^mc:\/\/file\//.test(value)) {
      if (client)
        void openDeviceFile(
          client,
          `/attachments/${encodeURIComponent(value.slice(10))}`,
        ).catch(showError);
      return true;
    }
    if (/^https?:/i.test(value)) {
      const url = remoteWebURL(value);
      if (!url) Alert.alert(t("web"), t("local_web"));
      else
        router.push({
          pathname: "/[workspace]/ai-sessions/[device]/side",
          params: { workspace, device, session, tab: "web", url, name },
        });
      return true;
    }
    if (/^(\/|[A-Za-z]:[\\/]|sandbox:)/.test(value)) {
      router.push({
        pathname: "/[workspace]/ai-sessions/[device]/side",
        params: {
          workspace,
          device,
          session,
          tab: "files",
          path: value.replace(/^sandbox:/, ""),
          name,
        },
      });
      return true;
    }
    return false;
  };
  const params = { workspace, device, session };
  return (
    <SafeAreaView
      edges={embedded ? [] : ["bottom"]}
      className="flex-1 bg-background"
    >
      {!embedded && (
        <Stack.Screen
          options={{
            headerShown: true,
            title: snapshot?.conversation.title || t("new"),
            headerTitle: () => (
              <View className="min-w-0 max-w-full items-center">
                <Text
                  accessibilityRole="header"
                  numberOfLines={1}
                  style={{ fontSize: 17, lineHeight: undefined }}
                  className="font-semibold"
                >
                  {snapshot?.conversation.title || t("new")}
                </Text>
                <Text
                  numberOfLines={1}
                  style={{ lineHeight: undefined }}
                  className="text-xs text-muted-foreground"
                >
                  {name || t("device_name")} ·{" "}
                  {t(connected ? "online" : "offline")}
                </Text>
              </View>
            ),
          }}
        />
      )}
      {!embedded && (
        <Stack.Toolbar placement="left">
          <Stack.Toolbar.View hidesSharedBackground>
            <IconButton
              name="chevron-back"
              hitSlop={4}
              accessibilityLabel={t("sessions")}
              onPress={() => router.back()}
            />
          </Stack.Toolbar.View>
        </Stack.Toolbar>
      )}
      {!embedded && (
        <Stack.Toolbar placement="right">
          <Stack.Toolbar.View hidesSharedBackground>
            <View className="flex-row">
              {snapshot?.conversation.turn_count === 0 && (
                <IconButton
                  name="folder-open-outline"
                  accessibilityLabel={t("select_project")}
                  disabled={!connected}
                  onPress={() =>
                    router.push({
                      pathname: "/[workspace]/ai-sessions/[device]/folders",
                      params,
                    })
                  }
                />
              )}
              <IconButton
                name="document-text-outline"
                accessibilityLabel={t("files")}
                onPress={() =>
                  router.push({
                    pathname: "/[workspace]/ai-sessions/[device]/side",
                    params: { ...params, name },
                  })
                }
              />
              <IconButton
                name="ellipsis-horizontal"
                accessibilityLabel={t("more_actions")}
                disabled={!connected || busy || !!pending}
                onPress={actions}
              />
            </View>
          </Stack.Toolbar.View>
        </Stack.Toolbar>
      )}
      {snapshot?.conversation.side_source_id && (
        <Pressable
          className="mx-4 my-2 p-3 rounded-md bg-secondary/60"
          accessibilityRole="button"
          onPress={() =>
            onOpenSource
              ? onOpenSource()
              : router.push({
                  pathname: "/[workspace]/ai-sessions/[device]/[session]",
                  params: {
                    workspace,
                    device,
                    session: snapshot.conversation.side_source_id!,
                    turn: snapshot.conversation.side_source_turn_id,
                    name,
                  },
                })
          }
        >
          <View className="flex-row items-center gap-1">
            <Text
              style={{ lineHeight: undefined }}
              className="text-xs text-muted-foreground"
            >
              {t("source")}
            </Text>
            <Ionicons
              name="chevron-forward"
              size={14}
              color={theme.mutedForeground}
            />
          </View>
          <Text style={{ lineHeight: undefined }} numberOfLines={3}>
            {snapshot.conversation.side_quote}
          </Text>
        </Pressable>
      )}
      <ConnectionNotice
        connected={connected}
        subscriptionRejected={subscriptionRejected}
        onRetrySubscription={() => client?.retrySubscription(session)}
      />
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        className="flex-1"
      >
        <View className="relative flex-1">
          {query.isPending ? (
            <ActivityIndicator className="mt-8" />
          ) : (
            <FlashList
              ref={list}
              data={allRows}
              keyExtractor={(item) => item.id}
              maintainVisibleContentPosition={{
                startRenderingFromBottom: true,
              }}
              onScroll={({ nativeEvent }) => {
                nearBottom.current =
                  nativeEvent.contentSize.height -
                    nativeEvent.contentOffset.y -
                    nativeEvent.layoutMeasurement.height <
                  100;
                setReadingHistory(!nearBottom.current);
              }}
              contentContainerStyle={{ padding: 16 }}
              keyboardShouldPersistTaps="handled"
              ListHeaderComponent={
                hasOlder || history.isError ? (
                  <Button
                    style={{ height: "auto", minHeight: 40 }}
                    hitSlop={2}
                    variant="ghost"
                    disabled={!connected || history.isFetchingNextPage}
                    onPress={() => {
                      nearBottom.current = false;
                      void history.fetchNextPage();
                    }}
                  >
                    <Text style={{ lineHeight: undefined }}>
                      {t(history.isError ? "retry" : "load_more")}
                    </Text>
                    {history.isFetchingNextPage && <ActivityIndicator />}
                  </Button>
                ) : null
              }
              ListEmptyComponent={
                <View className="items-center justify-center py-20 gap-4">
                  <MulticaLogo size={36} />
                  <Text
                    style={{ lineHeight: undefined }}
                    className="text-muted-foreground"
                  >
                    {t("empty_conversation")}
                  </Text>
                  {query.isError && (
                    <Button
                      style={{ height: "auto", minHeight: 40 }}
                      hitSlop={2}
                      variant="outline"
                      onPress={() => void query.refetch()}
                    >
                      <Text style={{ lineHeight: undefined }}>
                        {t("retry")}
                      </Text>
                    </Button>
                  )}
                </View>
              }
              renderItem={({ item }) =>
                item.role === "user" ? (
                  <View className="self-end max-w-[80%] rounded-xl bg-muted px-3.5 py-2 mb-5">
                    <AIMessageContent content={item.content} />
                    {item.attachmentIds?.map((id) => (
                      <SessionAttachment key={id} id={id} />
                    ))}
                  </View>
                ) : item.role === "assistant" ? (
                  <View className="mb-5 gap-2">
                    <Text
                      style={{ lineHeight: undefined }}
                      className="text-xs text-muted-foreground"
                    >
                      {snapshot?.conversation.provider === "claude"
                        ? "Claude Code"
                        : "Codex"}
                    </Text>
                    <SessionMarkdown
                      content={item.content}
                      session={session}
                      onResourcePress={resource}
                      selectionAction={{
                        label: t("side_continue"),
                        onSelect: (quote) => {
                          void createSide(quote, item.turnId);
                        },
                      }}
                    />
                  </View>
                ) : item.role === "activity" ? (
                  <AIActivityGroup
                    items={item.items}
                    expanded={expanded.has(item.id)}
                    onToggle={() =>
                      setExpanded((old) => {
                        const next = new Set(old);
                        if (next.has(item.id)) next.delete(item.id);
                        else next.add(item.id);
                        return next;
                      })
                    }
                  />
                ) : (
                  <Text
                    style={{ lineHeight: undefined }}
                    accessibilityRole="alert"
                    className="mb-4 text-sm text-destructive"
                  >
                    {item.content}
                  </Text>
                )
              }
              ListFooterComponent={
                <>
                  {submissionView.body.map((record) => (
                    <View
                      key={record.request.requestId}
                      className="self-end max-w-[80%] rounded-xl bg-muted px-3.5 py-2 mb-5"
                    >
                      <AIMessageContent
                        content={
                          record.request.prepared?.text ?? record.request.text
                        }
                      />
                    </View>
                  ))}
                  {snapshot?.pending_interactions.map((interaction) => (
                    <SessionQuestions
                      key={interaction.interaction_id}
                      interaction={interaction}
                      disabled={!connected || busy}
                      onAnswer={async (answer) => {
                        await client!.command(
                          session,
                          {
                            type: "answer",
                            interaction_id: interaction.interaction_id,
                            answer,
                          },
                          commandResultSchema,
                        );
                        void query.refetch();
                      }}
                    />
                  ))}
                </>
              }
            />
          )}
          {readingHistory && (
            <View
              pointerEvents="box-none"
              className="absolute inset-x-0 bottom-3 items-center"
            >
              <Button
                style={{ height: "auto", minHeight: 36 }}
                variant="outline"
                size="sm"
                hitSlop={4}
                className="rounded-full"
                accessibilityLabel={t("latest")}
                onPress={() => {
                  nearBottom.current = true;
                  setReadingHistory(false);
                  list.current?.scrollToEnd({ animated: true });
                }}
              >
                <Ionicons
                  name="arrow-down"
                  size={16}
                  color={theme.foreground}
                />
                <Text style={{ lineHeight: undefined }}>{t("latest")}</Text>
              </Button>
            </View>
          )}
        </View>
        {!!snapshot && queuedCount > 0 && (
          <Button
            style={{ height: "auto", minHeight: 40 }}
            hitSlop={2}
            variant="outline"
            className="mx-3 mb-2"
            onPress={() =>
              router.push({
                pathname: "/[workspace]/ai-sessions/[device]/queue",
                params,
              })
            }
          >
            <Text style={{ lineHeight: undefined }}>
              {t(snapshot.queue.paused ? "queued_paused" : "queued_count", {
                count: queuedCount,
              })}
            </Text>
            <Ionicons
              name="chevron-forward"
              size={14}
              color={theme.mutedForeground}
            />
          </Button>
        )}
        {outbox
          .filter(
            (record) =>
              (sendFailed && !record.confirmed) ||
              record.authority?.state === "recovery_required",
          )
          .map((record) => (
            <View key={record.request.requestId} className="px-4 py-2">
              <Text
                style={{ lineHeight: undefined }}
                className="text-sm text-muted-foreground"
                numberOfLines={2}
              >
                {record.request.text}
              </Text>
              {sendFailed && (
                <Text
                  style={{ lineHeight: undefined }}
                  accessibilityRole="alert"
                  className="text-sm text-destructive"
                >
                  {t("send_failed")}
                </Text>
              )}
              <Button
                variant="ghost"
                onPress={() => {
                  void transmit(record).catch(() => setSendFailed(true));
                }}
                disabled={
                  !connected || record.authority?.state === "recovery_required"
                }
              >
                <Text style={{ lineHeight: undefined }}>{t("retry")}</Text>
              </Button>
            </View>
          ))}
        {!!referenceError && (
          <Text
            style={{ lineHeight: undefined }}
            accessibilityRole="alert"
            className="px-4 py-2 text-sm text-destructive"
          >
            {referenceError}
          </Text>
        )}
        <View className="bg-background px-3 pt-2 pb-1">
          <View
            className="rounded-2xl border border-border bg-secondary"
            style={continuousCorners}
          >
            {(references.length > 0 || attachments.length > 0) && (
              <View className="px-2 pt-2 pb-1 gap-2">
                <ReferenceChips
                  references={references}
                  disabled={busy || !!pending}
                  onRemove={(reference) => {
                    setReferences(
                      draftKey,
                      references.filter(
                        (entry) =>
                          referenceKey(entry) !== referenceKey(reference),
                      ),
                    );
                    setReferenceError("");
                  }}
                />
                {!!attachments.length && (
                  <ScrollView
                    horizontal
                    keyboardShouldPersistTaps="handled"
                    contentContainerStyle={{ gap: 6, paddingVertical: 8 }}
                  >
                    {attachments.map((file) => (
                      <View
                        key={file.id}
                        className="max-w-64 min-h-7 flex-row items-center rounded-full bg-primary/10"
                      >
                        <Pressable
                          hitSlop={{ top: 8, bottom: 8 }}
                          accessibilityRole="button"
                          accessibilityLabel={file.name}
                          disabled={!connected || file.status !== "uploaded"}
                          className="shrink min-h-7 justify-center pl-2 py-1"
                          onPress={() => {
                            if (client)
                              void openDeviceFile(
                                client,
                                `/attachments/${encodeURIComponent(file.id)}`,
                                file.name,
                              ).catch(showError);
                          }}
                        >
                          <View className="flex-row items-center gap-1">
                            {file.status === "uploading" && (
                              <ActivityIndicator size="small" />
                            )}
                            <Text
                              style={{ lineHeight: undefined }}
                              numberOfLines={1}
                              className={cn(
                                "text-xs",
                                file.status === "failed" && "text-destructive",
                              )}
                            >
                              {file.name}
                            </Text>
                          </View>
                        </Pressable>
                        <IconButton
                          name="close"
                          iconSize={12}
                          hitSlop={8}
                          accessibilityLabel={t("remove_file", {
                            name: file.name,
                          })}
                          disabled={busy || !!pending}
                          className="h-7 w-7 rounded-full"
                          onPress={() => removeAttachment(file.id)}
                        />
                      </View>
                    ))}
                  </ScrollView>
                )}
              </View>
            )}
            {attachments.some((file) => file.status === "failed") && (
              <Text
                accessibilityRole="alert"
                className="px-4 text-sm text-destructive"
              >
                {t("attachment_failed")}
              </Text>
            )}
            <TextInput
              ref={input}
              multiline
              value={draft}
              onChangeText={changeDraft}
              onSelectionChange={(event) => {
                selection.current = event.nativeEvent.selection;
              }}
              editable={!busy && !pending && !snapshot?.conversation.archived}
              placeholder={t(rows.length ? "continue" : "message")}
              placeholderTextColor={theme.mutedForeground}
              accessibilityLabel={t("message")}
              className="px-4 pt-3 pb-1 text-foreground"
              style={{
                fontSize: 16,
                minHeight: 28,
                maxHeight: 140,
                textAlignVertical: "top",
              }}
            />
            {!!agents.length && (
              <Text
                style={{ lineHeight: undefined }}
                className="px-4 py-1 text-xs text-muted-foreground"
              >
                {t("references.invoke_hint", {
                  names: agents.map((entry) => entry.name).join(", "),
                })}
              </Text>
            )}
            <View className="flex-row items-center px-2 pb-2 pt-1">
              <IconButton
                name="at"
                className="h-8 w-8"
                hitSlop={6}
                color={references.length ? theme.primary : undefined}
                accessibilityLabel={t("references.title")}
                disabled={
                  !connected ||
                  busy ||
                  !!pending ||
                  !snapshot ||
                  snapshot.conversation.archived
                }
                onPress={() => openReferences()}
              />
              <IconButton
                name="attach-outline"
                className="h-8 w-8"
                hitSlop={6}
                accessibilityLabel={t("attachment")}
                disabled={
                  !connected ||
                  busy ||
                  !!pending ||
                  snapshot?.conversation.archived
                }
                onPress={() => void upload().catch(showError)}
              />
              <Button
                style={{ height: "auto", minHeight: 36 }}
                variant="ghost"
                size="sm"
                hitSlop={{ top: 4, bottom: 4 }}
                className="flex-1 min-w-0 shrink px-2"
                disabled={
                  !connected || running || busy || !!pending || !!agents.length
                }
                accessibilityLabel={t("settings")}
                onPress={() =>
                  router.push({
                    pathname: "/[workspace]/ai-sessions/[device]/models",
                    params: {
                      ...params,
                      provider: snapshot?.conversation.provider,
                    },
                  })
                }
              >
                <Text
                  style={{ lineHeight: undefined }}
                  className="shrink text-xs text-muted-foreground"
                  numberOfLines={1}
                >
                  {agents.length
                    ? t("agent_settings")
                    : `${snapshot?.conversation.provider === "claude" ? "Claude Code" : "Codex"} · ${snapshot?.settings.model || t("default_model")}`}
                </Text>
                <Ionicons
                  name="chevron-down"
                  size={12}
                  color={theme.mutedForeground}
                />
              </Button>
              <Button
                variant="default"
                size="icon"
                className="h-8 w-8 rounded-full"
                hitSlop={6}
                disabled={
                  busy ||
                  !connected ||
                  !snapshot ||
                  (!showStop && (!hasMessage || snapshot.conversation.archived))
                }
                accessibilityLabel={t(
                  showStop
                    ? "stop"
                    : sendFailed
                      ? "retry"
                      : running
                        ? "enqueue"
                        : "send",
                )}
                accessibilityHint={running ? t("working") : undefined}
                accessibilityState={{ busy }}
                onPress={() => void (showStop ? stop() : send())}
              >
                {busy ? (
                  <ActivityIndicator
                    size="small"
                    color={theme.primaryForeground}
                  />
                ) : (
                  <Ionicons
                    name={
                      showStop ? "stop" : sendFailed ? "refresh" : "arrow-up"
                    }
                    size={18}
                    color={theme.primaryForeground}
                  />
                )}
              </Button>
            </View>
          </View>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
