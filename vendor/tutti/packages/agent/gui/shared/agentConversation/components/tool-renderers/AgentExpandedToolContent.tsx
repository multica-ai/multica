import type { JSX } from "react";
import type { AgentToolCallVM } from "../../contracts/agentToolCallVM";
import { AgentApprovalContent } from "./AgentApprovalContent";
import { AgentAskUserQuestionContent } from "./AgentAskUserQuestionContent";
import { AgentBashContent } from "./AgentBashContent";
import { AgentEditContent } from "./AgentEditContent";
import { AgentImageGenerationContent } from "./AgentImageGenerationContent";
import { AgentMcpToolContent } from "./AgentMcpToolContent";
import { AgentPlanModeContent } from "./AgentPlanModeContent";
import { AgentReadContent } from "./AgentReadContent";
import { AgentSearchContent } from "./AgentSearchContent";
import { AgentSkillContent } from "./AgentSkillContent";
import { AgentTaskContent } from "./AgentTaskContent";
import { AgentTodoWriteContent } from "./AgentTodoWriteContent";
import { AgentToolSearchContent } from "./AgentToolSearchContent";
import { AgentDefaultToolContent } from "./agentToolContentShared";
import { AgentWebFetchContent } from "./AgentWebFetchContent";
import { AgentWebSearchContent } from "./AgentWebSearchContent";
import { AgentWriteContent } from "./AgentWriteContent";
import { observer } from "mobx-react-lite";
export const AgentExpandedToolContent = observer(
  AgentExpandedToolContentRender
);

function AgentExpandedToolContentRender({
  call,
  onLinkClick
}: {
  call: AgentToolCallVM;
  onLinkClick?: (href: string) => void;
}): JSX.Element | null {
  "use memo";
  const props = { call, onLinkClick };
  let content: JSX.Element | null;
  switch (call.rendererKind) {
    case "approval":
      content = <AgentApprovalContent {...props} />;
      break;
    case "plan-enter":
    case "plan-exit":
      content = <AgentPlanModeContent {...props} />;
      break;
    case "ask-user":
      content = <AgentAskUserQuestionContent {...props} />;
      break;
    case "task":
      content = <AgentTaskContent {...props} />;
      break;
    case "read":
      content = <AgentReadContent {...props} />;
      break;
    case "write":
      content = <AgentWriteContent {...props} />;
      break;
    case "edit":
      content = <AgentEditContent {...props} />;
      break;
    case "bash":
      content = <AgentBashContent {...props} />;
      break;
    case "search":
      content = <AgentSearchContent {...props} />;
      break;
    case "web-search":
      content = <AgentWebSearchContent {...props} />;
      break;
    case "web-fetch":
      content = <AgentWebFetchContent {...props} />;
      break;
    case "image-generation":
      content = <AgentImageGenerationContent {...props} />;
      break;
    case "todo-write":
      content = <AgentTodoWriteContent {...props} />;
      break;
    case "tool-search":
      content = <AgentToolSearchContent {...props} />;
      break;
    case "skill":
      content = <AgentSkillContent {...props} />;
      break;
    case "mcp":
      content = <AgentMcpToolContent {...props} />;
      break;
    default:
      content = <AgentDefaultToolContent {...props} />;
  }
  if (!content) {
    return null;
  }
  return content;
}
