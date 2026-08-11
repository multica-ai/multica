// Code generated from server/internal/cerebro/workflows/hook_field_manifest.json. DO NOT EDIT.

export const HOOK_FIELD_CATALOG = {
  "common": [
    {
      "path": "actor.type",
      "label": "Actor type",
      "input": "select",
      "options": [
        {
          "value": "member",
          "label": "Person"
        },
        {
          "value": "agent",
          "label": "Agent"
        },
        {
          "value": "system",
          "label": "System"
        }
      ]
    },
    {
      "path": "actor.id",
      "label": "Actor id",
      "input": "text"
    },
    {
      "path": "agent.id",
      "label": "Agent id",
      "input": "text"
    },
    {
      "path": "agent.model",
      "label": "Agent model",
      "input": "text"
    },
    {
      "path": "issue.id",
      "label": "Issue id",
      "input": "text"
    },
    {
      "path": "session.id",
      "label": "Session id",
      "input": "text"
    },
    {
      "path": "project.id",
      "label": "Project id",
      "input": "text"
    },
    {
      "path": "workflow.id",
      "label": "Workflow id",
      "input": "text"
    },
    {
      "path": "workspace.id",
      "label": "Workspace id",
      "input": "text"
    },
    {
      "path": "attempt",
      "label": "Attempt number",
      "input": "number"
    },
    {
      "path": "no_progress",
      "label": "Runs without progress",
      "input": "number"
    },
    {
      "path": "hook_depth",
      "label": "Hook depth",
      "input": "number"
    }
  ],
  "events": [
    {
      "type": "before.message.send",
      "fields": [
        {
          "path": "message.agent_authored",
          "label": "Message · agent authored",
          "input": "boolean"
        },
        {
          "path": "message.has_recipient",
          "label": "Message · has recipient",
          "input": "boolean"
        },
        {
          "path": "message.has_active_wakeup",
          "label": "Message · has active wakeup",
          "input": "boolean"
        },
        {
          "path": "message.promises_continuation",
          "label": "Message · promises continuation",
          "input": "boolean"
        },
        {
          "path": "message.thread_required",
          "label": "Message · thread required",
          "input": "boolean"
        },
        {
          "path": "message.correct_thread",
          "label": "Message · in correct thread",
          "input": "boolean"
        },
        {
          "path": "message.required_parent_id",
          "label": "Message · required parent id",
          "input": "text"
        },
        {
          "path": "message.no_action",
          "label": "Message · task recorded no_action",
          "input": "boolean"
        },
        {
          "path": "message.is_sub_issue",
          "label": "Message · is sub issue",
          "input": "boolean"
        },
        {
          "path": "message.mentions_initiator",
          "label": "Message · mentions initiator",
          "input": "boolean"
        },
        {
          "path": "message.mentions_agent",
          "label": "Message · mentions agent",
          "input": "boolean"
        },
        {
          "path": "message.posted_on_parent",
          "label": "Message · already posted on parent",
          "input": "boolean"
        }
      ]
    },
    {
      "type": "before.agent.stop",
      "fields": [
        {
          "path": "issue.status",
          "label": "Issue status",
          "input": "select",
          "options": [
            {
              "value": "backlog",
              "label": "Backlog"
            },
            {
              "value": "todo",
              "label": "To do"
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
              "value": "blocked",
              "label": "Blocked"
            },
            {
              "value": "done",
              "label": "Done"
            },
            {
              "value": "cancelled",
              "label": "Cancelled"
            }
          ]
        },
        {
          "path": "issue.terminal",
          "label": "Issue in terminal status",
          "input": "boolean"
        },
        {
          "path": "continuation.present",
          "label": "Continuation registered",
          "input": "boolean"
        },
        {
          "path": "continuation.kind",
          "label": "Continuation kind",
          "input": "text"
        },
        {
          "path": "continuation.evidence_id",
          "label": "Continuation evidence id",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.task.complete",
      "fields": [
        {
          "path": "issue.status",
          "label": "Issue status",
          "input": "select",
          "options": [
            {
              "value": "backlog",
              "label": "Backlog"
            },
            {
              "value": "todo",
              "label": "To do"
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
              "value": "blocked",
              "label": "Blocked"
            },
            {
              "value": "done",
              "label": "Done"
            },
            {
              "value": "cancelled",
              "label": "Cancelled"
            }
          ]
        },
        {
          "path": "issue.terminal",
          "label": "Issue in terminal status",
          "input": "boolean"
        },
        {
          "path": "continuation.present",
          "label": "Continuation registered",
          "input": "boolean"
        },
        {
          "path": "continuation.kind",
          "label": "Continuation kind",
          "input": "text"
        },
        {
          "path": "continuation.evidence_id",
          "label": "Continuation evidence id",
          "input": "text"
        }
      ]
    },
    {
      "type": "on.task.failure",
      "fields": [
        {
          "path": "failure.reason",
          "label": "Failure · reason",
          "input": "text"
        },
        {
          "path": "failure.message",
          "label": "Failure · message",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "failure.attempt",
          "label": "Failure · attempt",
          "input": "number"
        },
        {
          "path": "failure.max_attempts",
          "label": "Failure · max attempts",
          "input": "number"
        },
        {
          "path": "task.id",
          "label": "Task id",
          "input": "text"
        },
        {
          "path": "task.status",
          "label": "Task status",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.issue.assigned",
      "fields": [
        {
          "path": "assignment.agent_id",
          "label": "Assignment · agent id",
          "input": "text"
        },
        {
          "path": "assignment.reason",
          "label": "Assignment · reason",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.issue.status_change",
      "fields": [
        {
          "path": "status.from",
          "label": "Status · from",
          "input": "select",
          "options": [
            {
              "value": "backlog",
              "label": "Backlog"
            },
            {
              "value": "todo",
              "label": "To do"
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
              "value": "blocked",
              "label": "Blocked"
            },
            {
              "value": "done",
              "label": "Done"
            },
            {
              "value": "cancelled",
              "label": "Cancelled"
            }
          ]
        },
        {
          "path": "status.to",
          "label": "Status · to",
          "input": "select",
          "options": [
            {
              "value": "backlog",
              "label": "Backlog"
            },
            {
              "value": "todo",
              "label": "To do"
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
              "value": "blocked",
              "label": "Blocked"
            },
            {
              "value": "done",
              "label": "Done"
            },
            {
              "value": "cancelled",
              "label": "Cancelled"
            }
          ]
        },
        {
          "path": "chain.active",
          "label": "Chain · active",
          "input": "boolean"
        },
        {
          "path": "chain.approved_for_done",
          "label": "Chain · approved for Done",
          "input": "boolean"
        }
      ]
    },
    {
      "type": "after.workflow.step_completed",
      "fields": [
        {
          "path": "workflow.phase_id",
          "label": "Workflow · phase id",
          "input": "text"
        },
        {
          "path": "workflow.block_id",
          "label": "Workflow · block id",
          "input": "text"
        },
        {
          "path": "workflow.block_type",
          "label": "Workflow · block type",
          "input": "text"
        },
        {
          "path": "workflow.step_number",
          "label": "Workflow · step number",
          "input": "number"
        },
        {
          "path": "workflow.step_status",
          "label": "Workflow · step status",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.wakeup.create",
      "fields": [
        {
          "path": "wakeup.trigger_type",
          "label": "Wakeup · trigger type",
          "input": "text"
        },
        {
          "path": "wakeup.trigger_enabled",
          "label": "Wakeup · trigger enabled",
          "input": "boolean"
        },
        {
          "path": "wakeup.active_count",
          "label": "Wakeup · active count",
          "input": "number"
        },
        {
          "path": "wakeup.max_active",
          "label": "Wakeup · max active",
          "input": "number"
        },
        {
          "path": "wakeup.min_interval_seconds",
          "label": "Wakeup · min interval (seconds)",
          "input": "number"
        },
        {
          "path": "wakeup.seconds_until_fire",
          "label": "Wakeup · seconds until fire",
          "input": "number"
        },
        {
          "path": "wakeup.has_last_fire",
          "label": "Wakeup · has fired before",
          "input": "boolean"
        },
        {
          "path": "wakeup.seconds_after_last_fire",
          "label": "Wakeup · seconds after last fire",
          "input": "number"
        },
        {
          "path": "wakeup.loop_limit_enabled",
          "label": "Wakeup · loop limit enabled",
          "input": "boolean"
        },
        {
          "path": "wakeup.consecutive_without_progress",
          "label": "Wakeup · consecutive without progress",
          "input": "number"
        },
        {
          "path": "wakeup.max_without_progress",
          "label": "Wakeup · max without progress",
          "input": "number"
        },
        {
          "path": "wakeup.since_member_reply",
          "label": "Wakeup · wakeups since member reply",
          "input": "number"
        },
        {
          "path": "wakeup.since_status_change",
          "label": "Wakeup · wakeups since status change",
          "input": "number"
        },
        {
          "path": "wakeup.since_progress_update",
          "label": "Wakeup · wakeups since progress update",
          "input": "number"
        },
        {
          "path": "wakeup.since_pull_request_update",
          "label": "Wakeup · wakeups since pull request update",
          "input": "number"
        },
        {
          "path": "wakeup.expected_continuation",
          "label": "Wakeup · expected continuation",
          "input": "text"
        }
      ]
    },
    {
      "type": "on.wakeup.fire_failure",
      "fields": [
        {
          "path": "failure.reason",
          "label": "Failure · reason",
          "input": "text"
        },
        {
          "path": "failure.message",
          "label": "Failure · message",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "failure.consecutive_postpones",
          "label": "Failure · consecutive postpones",
          "input": "number"
        },
        {
          "path": "failure.next_consecutive_postpone",
          "label": "Failure · next consecutive postpone",
          "input": "number"
        },
        {
          "path": "wakeup.id",
          "label": "Wakeup · id",
          "input": "text"
        },
        {
          "path": "wakeup.trigger_type",
          "label": "Wakeup · trigger type",
          "input": "text"
        },
        {
          "path": "wakeup.expected_continuation",
          "label": "Wakeup · expected continuation",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.session.start",
      "fields": [
        {
          "path": "provider",
          "label": "Provider",
          "input": "text"
        },
        {
          "path": "resuming",
          "label": "Resuming",
          "input": "boolean"
        }
      ]
    },
    {
      "type": "after.session.start",
      "fields": [
        {
          "path": "resuming",
          "label": "Resuming",
          "input": "boolean"
        }
      ]
    },
    {
      "type": "before.session.end",
      "fields": [
        {
          "path": "status",
          "label": "Session · result status",
          "input": "text"
        },
        {
          "path": "error",
          "label": "Session · result error",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "handoff.root_comment_id",
          "label": "Handoff · root comment id",
          "input": "text"
        },
        {
          "path": "handoff.start_new",
          "label": "Handoff · start new session",
          "input": "boolean"
        },
        {
          "path": "handoff.prompt",
          "label": "Handoff · prompt",
          "input": "text",
          "sensitive": true
        }
      ]
    },
    {
      "type": "after.session.end",
      "fields": [
        {
          "path": "status",
          "label": "Session · result status",
          "input": "text"
        },
        {
          "path": "error",
          "label": "Session · result error",
          "input": "text",
          "sensitive": true
        }
      ]
    },
    {
      "type": "before.prompt.assemble",
      "fields": [
        {
          "path": "prompt",
          "label": "Prompt",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "provider",
          "label": "Provider",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.tool.call",
      "fields": []
    },
    {
      "type": "after.tool.call",
      "fields": [
        {
          "path": "tool.name",
          "label": "Tool · name",
          "input": "text"
        },
        {
          "path": "tool.output",
          "label": "Tool · output",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "call_id",
          "label": "Call id",
          "input": "text"
        }
      ]
    },
    {
      "type": "on.tool.failure",
      "fields": [
        {
          "path": "tool.name",
          "label": "Tool · name",
          "input": "text"
        },
        {
          "path": "tool.error",
          "label": "Tool · error",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "task.id",
          "label": "Task id",
          "input": "text"
        },
        {
          "path": "task.status",
          "label": "Task status",
          "input": "text"
        }
      ]
    },
    {
      "type": "before.subagent.start",
      "fields": []
    },
    {
      "type": "after.subagent.stop",
      "fields": []
    },
    {
      "type": "on.error",
      "fields": [
        {
          "path": "error",
          "label": "Error message",
          "input": "text",
          "sensitive": true
        },
        {
          "path": "source",
          "label": "Error source",
          "input": "text"
        }
      ]
    }
  ]
} as const;
