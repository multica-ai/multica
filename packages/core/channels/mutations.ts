import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { channelKeys } from "./queries";
import { createLogger } from "../logger";
import type { Channel, CreateChannelRequest } from "../types";

const logger = createLogger("channels.mut");

export function useCreateChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: CreateChannelRequest): Promise<Channel> => {
      logger.info("createChannel.start", {
        kind: data.kind,
        memberCount: data.member_ids.length,
        agentCount: data.agent_ids.length,
      });
      return api.createChannel(data);
    },
    onSuccess: (channel) => {
      logger.info("createChannel.success", {
        channelId: channel.id,
        kind: channel.kind,
      });
    },
    onError: (err) => {
      logger.error("createChannel.error", err);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}
