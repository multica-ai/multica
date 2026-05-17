package daemon

import "testing"

func TestResolveTaskModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		task       Task
		entryModel string
		want       string
	}{
		{
			name:       "agent override wins",
			task:       Task{Agent: &AgentData{Model: "claude-opus-4-7"}},
			entryModel: "claude-sonnet-4-6",
			want:       "claude-opus-4-7",
		},
		{
			name:       "entry default used when agent omits model",
			task:       Task{Agent: &AgentData{}},
			entryModel: "claude-sonnet-4-6",
			want:       "claude-sonnet-4-6",
		},
		{
			name:       "nil agent falls through to entry default",
			task:       Task{},
			entryModel: "gpt-5",
			want:       "gpt-5",
		},
		{
			name:       "both empty returns empty",
			task:       Task{Agent: &AgentData{}},
			entryModel: "",
			want:       "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveTaskModel(tc.task, tc.entryModel); got != tc.want {
				t.Fatalf("resolveTaskModel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTriggerSource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		task Task
		want string
	}{
		{
			name: "default is manual",
			task: Task{ID: "t1"},
			want: "manual",
		},
		{
			name: "autopilot wins over every other marker",
			task: Task{
				AutopilotRunID:    "ar-1",
				ChatSessionID:     "chat-1",
				QuickCreatePrompt: "draft me a release note",
				SquadID:           "squad-1",
			},
			want: "autopilot",
		},
		{
			name: "chat wins over quick_create and squad",
			task: Task{
				ChatSessionID:     "chat-1",
				QuickCreatePrompt: "draft me a release note",
				SquadID:           "squad-1",
			},
			want: "chat",
		},
		{
			name: "quick_create wins over squad",
			task: Task{
				QuickCreatePrompt: "draft me a release note",
				SquadID:           "squad-1",
			},
			want: "quick_create",
		},
		{
			name: "squad routes to squad",
			task: Task{SquadID: "squad-1"},
			want: "squad",
		},
		{
			name: "comment-triggered manual run",
			task: Task{
				IssueID:               "issue-1",
				TriggerCommentID:      "comment-1",
				TriggerCommentContent: "please fix",
			},
			want: "manual",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := triggerSource(tc.task); got != tc.want {
				t.Fatalf("triggerSource = %q, want %q", got, tc.want)
			}
		})
	}
}
