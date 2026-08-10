// Code generated from server/internal/cerebro/workflows/hook_action_manifest.json. DO NOT EDIT.

export const HOOK_ACTION_CATALOG = [
  {
    "type": "member.notify",
    "label": "Notify member",
    "description": "Send a clear notification to a named member.",
    "capability": "add_comment",
    "fields": [
      {
        "key": "member_id",
        "label": "Member",
        "input": "target",
        "target": "member",
        "required": true,
        "summary": "target"
      },
      {
        "key": "title",
        "label": "Title",
        "input": "text",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "message",
        "label": "Message",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "agent.dispatch",
    "label": "Instruct an agent",
    "description": "Give an agent an instruction and start it. Pair with Guide to instruct instead of stopping.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "agent_id",
        "label": "Agent",
        "input": "target",
        "target": "agent",
        "required": true,
        "summary": "target",
        "event_target": "event.agent"
      },
      {
        "key": "prompt",
        "label": "Instruction",
        "input": "textarea",
        "summary": "redacted",
        "help": "What the agent should do next, for example: try again and address the rule this hook checks."
      }
    ]
  },
  {
    "type": "squad.dispatch",
    "label": "Start squad",
    "description": "Start a squad through its lead agent.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "squad_id",
        "label": "Squad",
        "input": "target",
        "target": "squad",
        "required": true,
        "summary": "target"
      },
      {
        "key": "agent_id",
        "label": "Lead agent",
        "input": "target",
        "target": "agent",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "skill.run",
    "label": "Run skill",
    "description": "Run a named skill, optionally with a specific agent.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "skill_name",
        "label": "Skill",
        "input": "target",
        "target": "skill",
        "required": true,
        "summary": "target"
      },
      {
        "key": "agent_id",
        "label": "Agent",
        "input": "target",
        "target": "agent",
        "summary": "target"
      }
    ]
  },
  {
    "type": "judge.gate",
    "label": "Judge gate",
    "description": "Ask a judge agent to decide against a written rubric.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "agent_id",
        "label": "Judge agent",
        "input": "target",
        "target": "agent",
        "required": true,
        "summary": "target"
      },
      {
        "key": "rubric",
        "label": "Rubric",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "quality.gate",
    "label": "Quality gate",
    "description": "Judge the proposed comment text against a rubric live (synchronous). Bad comments are rejected with a rewrite requirement; good ones pass. fail_mode only governs judge outages.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "rubric",
        "label": "Rubric",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "eval.run",
    "label": "Run eval",
    "description": "Run a Cerebro eval when this event fires.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "eval_id",
        "label": "Eval",
        "input": "target",
        "target": "eval",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "eval.gate",
    "label": "Eval gate",
    "description": "Block this Hook unless the eval's latest run passed.",
    "capability": "trigger_other_agent",
    "fields": [
      {
        "key": "eval_id",
        "label": "Eval",
        "input": "target",
        "target": "eval",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "wakeup.create",
    "label": "Create wakeup",
    "description": "Schedule a single future wakeup.",
    "capability": "schedule_agent_wakeup",
    "fields": [
      {
        "key": "fire_at",
        "label": "Wake up at",
        "input": "datetime-local",
        "required": true,
        "summary": "safe"
      },
      {
        "key": "agent_id",
        "label": "Agent",
        "input": "target",
        "target": "agent",
        "summary": "target"
      },
      {
        "key": "issue_id",
        "label": "Issue",
        "input": "target",
        "target": "issue",
        "summary": "target"
      },
      {
        "key": "prompt",
        "label": "Prompt",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "wakeup.cancel",
    "label": "Cancel wakeup",
    "description": "Cancel one existing wakeup by its runtime value.",
    "capability": "schedule_agent_wakeup",
    "fields": [
      {
        "key": "wakeup_id",
        "label": "Wakeup ID",
        "input": "text",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "session.handoff",
    "label": "Start Handoff",
    "description": "Pass the work and its state to another agent session.",
    "capability": "manage_sessions",
    "fields": [
      {
        "key": "target",
        "label": "Target agent",
        "input": "target",
        "target": "agent",
        "required": true,
        "summary": "target"
      },
      {
        "key": "plan_ref",
        "label": "Plan reference",
        "input": "target",
        "target": "artifact",
        "summary": "target",
        "help": "Optional when the Hook does not follow a saved plan."
      },
      {
        "key": "start_new",
        "label": "Start new session now",
        "input": "checkbox",
        "summary": "safe"
      },
      {
        "key": "summary",
        "label": "Summary",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "done",
        "label": "Done",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "remaining",
        "label": "Remaining",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "max_depth",
        "label": "Maximum Handoff depth",
        "input": "number",
        "required": true,
        "summary": "safe"
      }
    ]
  },
  {
    "type": "task.retry",
    "label": "Repeat current step",
    "description": "Retry the task that produced the event.",
    "capability": "manage_work_sessions",
    "fields": [
      {
        "key": "task_id",
        "label": "Task ID",
        "input": "text",
        "required": true,
        "summary": "redacted",
        "event_target": "event.task"
      }
    ]
  },
  {
    "type": "task.cancel",
    "label": "Cancel task",
    "description": "Cancel the task that produced the event.",
    "capability": "manage_work_sessions",
    "fields": [
      {
        "key": "task_id",
        "label": "Task ID",
        "input": "text",
        "required": true,
        "summary": "redacted",
        "event_target": "event.task"
      }
    ]
  },
  {
    "type": "artifact.create_or_update",
    "label": "Create or update artifact",
    "description": "Write a reusable document from the Hook outcome.",
    "capability": "manage_artifacts",
    "fields": [
      {
        "key": "artifact_id",
        "label": "Existing artifact",
        "input": "target",
        "target": "artifact",
        "summary": "target"
      },
      {
        "key": "title",
        "label": "Title",
        "input": "text",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "kind",
        "label": "Type",
        "input": "select",
        "required": true,
        "summary": "safe",
        "options": [
          {
            "value": "report",
            "label": "Report"
          },
          {
            "value": "plan",
            "label": "Plan"
          },
          {
            "value": "decision",
            "label": "Decision"
          },
          {
            "value": "diagram",
            "label": "Diagram"
          },
          {
            "value": "note",
            "label": "Note"
          }
        ]
      },
      {
        "key": "body",
        "label": "Content",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "workflow.activate",
    "label": "Start workflow",
    "description": "Start a named workflow.",
    "capability": "manage_workflows",
    "fields": [
      {
        "key": "workflow_id",
        "label": "Workflow",
        "input": "target",
        "target": "workflow",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "workflow.pause",
    "label": "Pause workflow",
    "description": "Pause a named workflow.",
    "capability": "manage_workflows",
    "fields": [
      {
        "key": "workflow_id",
        "label": "Workflow",
        "input": "target",
        "target": "workflow",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "workflow.resume",
    "label": "Resume workflow",
    "description": "Resume a named workflow.",
    "capability": "manage_workflows",
    "fields": [
      {
        "key": "workflow_id",
        "label": "Workflow",
        "input": "target",
        "target": "workflow",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "workflow.stop",
    "label": "Stop workflow",
    "description": "Stop a named workflow.",
    "capability": "manage_workflows",
    "fields": [
      {
        "key": "workflow_id",
        "label": "Workflow",
        "input": "target",
        "target": "workflow",
        "required": true,
        "summary": "target"
      }
    ]
  },
  {
    "type": "approval.require",
    "label": "Require approval",
    "description": "Pause until a person approves the described capability.",
    "capability": "decide_approval",
    "fields": [
      {
        "key": "capability",
        "label": "Capability",
        "input": "text",
        "required": true,
        "summary": "safe"
      },
      {
        "key": "resource",
        "label": "Resource",
        "input": "text",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "reason",
        "label": "Reason",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "issue.comment",
    "label": "Comment on issue",
    "description": "Post a comment on the event's issue. Supports {{placeholders}} like {{task.failure_reason}}.",
    "capability": "add_comment",
    "fields": [
      {
        "key": "body",
        "label": "Comment",
        "input": "textarea",
        "required": true,
        "summary": "redacted"
      },
      {
        "key": "issue_id",
        "label": "Issue",
        "input": "target",
        "target": "issue",
        "summary": "target",
        "help": "Optional; defaults to the issue that produced the event."
      }
    ]
  },
  {
    "type": "issue.status",
    "label": "Set issue status",
    "description": "Change the event's issue to a named status.",
    "capability": "update_issue",
    "fields": [
      {
        "key": "status",
        "label": "Status",
        "input": "select",
        "required": true,
        "summary": "safe",
        "options": [
          {
            "value": "backlog",
            "label": "Backlog"
          },
          {
            "value": "todo",
            "label": "Todo"
          },
          {
            "value": "in_progress",
            "label": "In progress"
          },
          {
            "value": "in_review",
            "label": "In review"
          },
          {
            "value": "done",
            "label": "Done"
          },
          {
            "value": "blocked",
            "label": "Blocked"
          },
          {
            "value": "cancelled",
            "label": "Cancelled"
          }
        ]
      },
      {
        "key": "issue_id",
        "label": "Issue",
        "input": "target",
        "target": "issue",
        "summary": "target",
        "help": "Optional; defaults to the issue that produced the event."
      }
    ]
  },
  {
    "type": "issue.check_related",
    "label": "Check for a related issue",
    "description": "Look for an existing issue on the same matter, link every match, and comment the first time a link is made.",
    "capability": "add_comment",
    "fields": [
      {
        "key": "link",
        "label": "Link matching issues",
        "input": "checkbox",
        "default": true,
        "summary": "safe",
        "help": "On by default. Each match is recorded as a related issue."
      },
      {
        "key": "comment",
        "label": "Comment the first time a link is made",
        "input": "checkbox",
        "default": true,
        "summary": "safe",
        "help": "On by default. Repeat runs on the same issue stay silent."
      },
      {
        "key": "block_on_duplicate",
        "label": "Stop the hand-over on a duplicate",
        "input": "checkbox",
        "default": false,
        "summary": "safe",
        "help": "Off by default. Left off the agent still starts, with the duplicate spelled out on the issue."
      },
      {
        "key": "issue_id",
        "label": "Issue",
        "input": "target",
        "target": "issue",
        "summary": "target",
        "help": "Optional; defaults to the issue that produced the event."
      }
    ]
  },
  {
    "type": "audit.record",
    "label": "Record audit event",
    "description": "Add a named event to the audit trail.",
    "fields": [
      {
        "key": "event",
        "label": "Event name",
        "input": "text",
        "required": true,
        "summary": "safe"
      },
      {
        "key": "message",
        "label": "Details",
        "input": "textarea",
        "summary": "redacted"
      }
    ]
  },
  {
    "type": "metric.increment",
    "label": "Increment metric",
    "description": "Increase a named operational metric.",
    "fields": [
      {
        "key": "name",
        "label": "Metric name",
        "input": "text",
        "required": true,
        "summary": "safe"
      },
      {
        "key": "amount",
        "label": "Amount",
        "input": "number",
        "required": true,
        "summary": "safe"
      }
    ]
  }
] as const;
