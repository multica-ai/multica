import { useMemo } from "react";
import { Avatar } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
export const AgentGUIOwnerAvatar = observer(AgentGUIOwnerAvatarRender);

interface AgentGUIOwnerAvatarProps {
  className: string;
  iconUrl: string;
  imageClassName?: string;
  label?: string | null;
}

function AgentGUIOwnerAvatarRender({
  className,
  iconUrl,
  imageClassName,
  label
}: AgentGUIOwnerAvatarProps): React.JSX.Element {
  const ownerLabel = label?.trim() || "?";

  const styleProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof Avatar>["style"]>
  >(() => ({ height: "100%", width: "100%" }), []);
  return (
    <span
      aria-hidden="true"
      className={className}
      data-agent-owner-badge="true"
    >
      <Avatar
        aria-hidden="true"
        imageClassName={imageClassName}
        label={ownerLabel}
        size="md"
        src={iconUrl}
        style={styleProp1}
      />
    </span>
  );
}
