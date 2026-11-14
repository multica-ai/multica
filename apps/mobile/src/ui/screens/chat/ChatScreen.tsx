import { useEffect, useMemo, useState } from "react";
import { Alert } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { upsertChatMessageToCaches } from "@multica/core/chat/message-cache";
import { useMarkChatSessionRead } from "@multica/core/chat/mutations";
import { chatKeys, chatMessagesOptions, chatSessionOptions, pendingChatTaskOptions, useTaskMessages } from "@multica/core/chat/queries";
import type { ChatMessage, ChatPendingTask } from "@multica/core/types";
import { StepsDisclosure } from "../../components/StepsDisclosure";
import { runSteps } from "../../model/steps";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useDetail } from "../../shell/detail";
import { useStrings } from "../../strings";
import { ChatView, type ChatItem } from "./ChatView";

const VISIBLE_STEPS = 4;

function TaskSteps({ taskId }: { taskId: string }) {
  const strings = useStrings();
  const [requested, setRequested] = useState(false);
  const messages = useTaskMessages(taskId, false, requested);
  const steps = messages.data ? runSteps(messages.data, false) : null;
  if (messages.data && steps?.length === 0) return null;
  return <StepsDisclosure label={steps ? strings.chat.steps(steps.length) : strings.chat.process} steps={steps} onOpen={() => setRequested(true)} />;
}

export function ChatScreen() {
  const { sessionId } = useRoute<RouteProp<RootStackParamList, "Chat">>().params;
  return <ChatDetail sessionId={sessionId} />;
}

/** A chat by id: a pushed screen of its own, or the detail column beside the agents list. */
export function ChatDetail({ sessionId }: { sessionId: string }) {
  const navigation = useNavigation();
  const detail = useDetail();
  const strings = useStrings();
  const queryClient = useQueryClient();
  const { workspace } = useWorkspace();
  const session = useQuery(chatSessionOptions(workspace.id, sessionId));
  const messages = useQuery(chatMessagesOptions(sessionId));
  const pendingTask = useQuery(pendingChatTaskOptions(sessionId));
  const taskId = pendingTask.data?.task_id ?? "";
  const live = useTaskMessages(taskId, true, Boolean(taskId));
  const markRead = useMarkChatSessionRead().mutate;

  const lastMessageId = messages.data?.at(-1)?.id;
  useEffect(() => {
    if (lastMessageId) markRead(sessionId);
  }, [lastMessageId, sessionId, markRead]);

  const items = useMemo<ChatItem[]>(
    () =>
      (messages.data ?? []).map((message) =>
        message.role === "user"
          ? { kind: "user", id: message.id, text: message.content }
          : { kind: "assistant", id: message.id, text: message.content, taskId: message.task_id, failed: Boolean(message.failure_reason) },
      ),
    [messages.data],
  );

  const pending = useMemo(() => {
    if (!taskId) return null;
    const steps = runSteps(live.data ?? [], true);
    return { steps: steps.slice(-VISIBLE_STEPS), hiddenSteps: Math.max(0, steps.length - VISIBLE_STEPS) };
  }, [taskId, live.data]);

  const send = async (text: string) => {
    try {
      const result = await api.sendChatMessage(sessionId, text);
      const sent: ChatMessage = { id: result.message_id, chat_session_id: sessionId, role: "user", content: text, task_id: result.task_id, created_at: result.created_at };
      upsertChatMessageToCaches(queryClient, sessionId, sent, { seedIfMissing: true });
      queryClient.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), (current) => current?.task_id ? current : { task_id: result.task_id, status: "queued", created_at: result.created_at });
      void queryClient.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      void queryClient.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
    } catch (error) {
      Alert.alert(strings.chat.sendFailed);
      throw error;
    }
  };

  return (
    <ChatView
      title={session.data?.title || strings.agents.newChat}
      state={messages.data ? "ready" : messages.isError ? "error" : "loading"}
      items={items}
      pending={pending}
      onBack={() => navigation.goBack()}
      onRetry={() => void messages.refetch()}
      onSend={send}
      onOpenIssue={(issueId) => detail.open({ kind: "issue", issueId })}
      renderSteps={(id) => <TaskSteps taskId={id} />}
    />
  );
}
