export interface Channel {
  id: string;
  name: string;
  provider: "slack";
  created_at: string;
}

export interface IssueChannel {
  id: string;
  issue_id: string;
  channel_id: string;
  name: string;
  provider: string;
  has_thread: boolean;
  created_at: string;
}

export interface ChannelMessage {
  id: string;
  direction: "outbound" | "inbound";
  content: string;
  sender_type: "agent" | "user";
  created_at: string;
}

export interface CreateChannelRequest {
  name: string;
  provider: "slack";
  config: {
    bot_token: string;
    channel_id: string;
  };
}
