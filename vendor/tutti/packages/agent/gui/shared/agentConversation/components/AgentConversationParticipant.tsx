import type { JSX } from "react";
import { Avatar } from "@tutti-os/ui-system";
import type { AgentMessageRowVM } from "../contracts/agentMessageRowVM";
import type {
  AgentConversationParticipantIdentity,
  AgentConversationParticipantPresentation
} from "../contracts/agentConversationParticipantPresentation";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { observer } from "mobx-react-lite";
export const AgentConversationParticipantHeader = observer(
  AgentConversationParticipantHeaderRender
);
const AgentConversationParticipantAvatar = observer(
  AgentConversationParticipantAvatarRender
);

function AgentConversationParticipantHeaderRender({
  presentation,
  speaker
}: {
  presentation: Extract<
    AgentConversationParticipantPresentation,
    { enabled: true }
  >;
  speaker: AgentMessageRowVM["speaker"];
}): JSX.Element {
  const participant: AgentConversationParticipantIdentity | null =
    presentation.status === "loading"
      ? null
      : speaker === "user"
        ? presentation.user
        : presentation.agent;
  const nameContent = participant ? (
    <span className={styles.participantName}>{participant.name}</span>
  ) : null;
  const avatarContent = (
    <AgentConversationParticipantAvatar
      presentation={presentation}
      speaker={speaker}
    />
  );
  return (
    <div
      className={styles.participantMessageHeader}
      data-agent-conversation-participant-header={speaker}
    >
      {speaker === "user" ? (
        <>
          {nameContent}
          {avatarContent}
        </>
      ) : (
        <>
          {avatarContent}
          {nameContent}
        </>
      )}
    </div>
  );
}

function AgentConversationParticipantAvatarRender({
  presentation,
  speaker
}: {
  presentation: Extract<
    AgentConversationParticipantPresentation,
    { enabled: true }
  >;
  speaker: AgentMessageRowVM["speaker"];
}): JSX.Element {
  if (presentation.status === "loading") {
    return (
      <Avatar
        aria-hidden="true"
        className={styles.participantAvatar}
        data-agent-conversation-participant-avatar={speaker}
        label=""
        loading
        size={28}
      />
    );
  }

  const participant: AgentConversationParticipantIdentity =
    speaker === "user" ? presentation.user : presentation.agent;
  return (
    <Avatar
      aria-label={participant.name}
      className={styles.participantAvatar}
      data-agent-conversation-participant-avatar={speaker}
      label={participant.name}
      size={28}
      src={participant.avatarUrl}
    />
  );
}
