"use client";

import React from "react";
import { Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { Agent, ChatSession } from "@multica/core/types";
import { SessionListItem } from "./session-list-item";

interface SessionListProps {
  sessions: ChatSession[];
  agents: Agent[];
  activeSessionId: string | null;
  onSelectSession: (sessionId: string) => void;
}

export function SessionList({
  sessions,
  agents,
  activeSessionId,
  onSelectSession,
}: SessionListProps) {
  // 按更新时间倒序排列
  const sortedSessions = [...sessions].sort(
    (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
  );

  // 创建 agent ID 到 agent 的映射
  const agentById = new Map(agents.map((a) => [a.id, a]));

  return (
    <div className="w-80 flex flex-col border-r bg-muted/20">
      {/* 头部 */}
      <div className="flex items-center justify-between px-4 py-3 border-b">
        <h2 className="text-sm font-semibold">会话列表</h2>
        <Button
          variant="ghost"
          size="icon-sm"
          className="rounded-full"
          onClick={() => {
            // TODO: 创建新会话
            console.log("Create new session");
          }}
        >
          <Plus className="size-4" />
        </Button>
      </div>

      {/* 会话列表 */}
      <div className="flex-1 overflow-y-auto p-2 space-y-1">
        {sortedSessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-center px-4">
            <p className="text-sm text-muted-foreground">暂无会话</p>
            <p className="text-xs text-muted-foreground mt-1">点击右上角 + 创建新会话</p>
          </div>
        ) : (
          sortedSessions.map((session) => {
            const agent = agentById.get(session.agent_id);
            return (
              <SessionListItem
                key={session.id}
                session={session}
                agent={agent}
                isActive={session.id === activeSessionId}
                onClick={() => onSelectSession(session.id)}
              />
            );
          })
        )}
      </div>
    </div>
  );
}
