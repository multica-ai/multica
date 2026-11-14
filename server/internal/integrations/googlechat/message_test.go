package googlechat

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatNotificationsEscapesUserTextAndBuildsIssueLink(t *testing.T) {
	t.Parallel()

	got := FormatNotifications("Acme <Ops>", []NotificationItem{{
		IssueIdentifier: "ACME-12",
		Title:           "Fix <checkout> & alerts",
		Body:            "Ada > Bob\nneeds review",
		IssueURL:        "https://app.example.com/acme/issues/ACME-12",
	}})
	want := "*Acme &lt;Ops&gt;*\n" +
		"• <https://app.example.com/acme/issues/ACME-12|ACME-12: Fix &lt;checkout&gt; &amp; alerts> — Ada &gt; Bob needs review"
	if got != want {
		t.Fatalf("FormatNotifications() =\n%s\nwant\n%s", got, want)
	}
}

func TestNotificationActions(t *testing.T) {
	for _, test := range []struct {
		kind, want string
	}{
		{"issue_assigned", "Alice &lt;Ops&gt; assigned you"},
		{"mentioned", "Alice &lt;Ops&gt; mentioned you"},
		{"new_comment", "Alice &lt;Ops&gt; commented"},
		{"status_changed", "Alice &lt;Ops&gt; changed status: todo → done"},
		{"priority_changed", "Alice &lt;Ops&gt; changed priority: todo → done"},
		{"task_failed", "Alice &lt;Ops&gt;'s run failed"},
		{"future_event", "future event"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			got := FormatNotifications("Acme", []NotificationItem{{
				Title: "Fix login", Type: test.kind, Actor: "Alice <Ops>",
				Details: map[string]string{"from": "todo", "to": "done"},
			}})
			if !strings.Contains(got, test.want) || strings.Contains(got, "1 new notification") {
				t.Fatalf("message = %q, want action %q without count header", got, test.want)
			}
		})
	}
}

func TestFormatNotificationsRendersOneLinePerItem(t *testing.T) {
	t.Parallel()

	got := FormatNotifications("Acme", []NotificationItem{
		{IssueIdentifier: "ACME-12", Title: "First"},
		{Title: "Second", Body: "Needs attention"},
	})
	if !strings.HasPrefix(got, "*2 new notifications in Acme*") {
		t.Fatalf("unexpected heading: %q", got)
	}
	if lines := strings.Split(got, "\n"); len(lines) != 3 {
		t.Fatalf("line count = %d, want 3: %q", len(lines), got)
	}
}

func TestFormatNotificationsLimits(t *testing.T) {
	t.Parallel()

	exactBase := FormatNotifications("Acme", []NotificationItem{{Title: "Boundary"}})
	exactURLLength := googleChatMessageRuneBudget - utf8.RuneCountInString(exactBase) - 3
	if exactURLLength <= 0 {
		t.Fatal("exact-boundary fixture has no URL budget")
	}

	hundredItems := make([]NotificationItem, 100)
	for index := range hundredItems {
		hundredItems[index] = NotificationItem{
			IssueIdentifier: fmt.Sprintf("ACME-%d", index+1),
			Title:           strings.Repeat("T", 250),
			Body:            strings.Repeat("B", 250),
			IssueURL:        fmt.Sprintf("https://app.example.com/acme/issues/ACME-%d", index+1),
		}
	}

	tests := []struct {
		name  string
		items []NotificationItem
		check func(*testing.T, string)
	}{
		{
			name: "long body",
			items: []NotificationItem{{
				Title: "Body truncation",
				Body:  strings.Repeat("<", 250),
			}},
			check: func(t *testing.T, got string) {
				t.Helper()
				if strings.Count(got, "&lt;") != notificationFieldRuneLimit-1 ||
					!strings.HasSuffix(got, "…") {
					t.Fatalf("body was not safely truncated: %q", got)
				}
			},
		},
		{
			name: "long linked title",
			items: []NotificationItem{{
				Title:    strings.Repeat("&", 250),
				IssueURL: "https://app.example.com/acme/issues/ACME-12",
			}},
			check: func(t *testing.T, got string) {
				t.Helper()
				if strings.Count(got, "&amp;") != notificationFieldRuneLimit-1 ||
					!strings.HasSuffix(got, "…>") {
					t.Fatalf("linked label was cut inside markup or an entity: %q", got)
				}
			},
		},
		{
			name:  "one hundred items",
			items: hundredItems,
			check: func(t *testing.T, got string) {
				t.Helper()
				if !strings.Contains(got, " more in Multica") {
					t.Fatalf("omitted-item summary missing: %q", got)
				}
			},
		},
		{
			name: "exact boundary",
			items: []NotificationItem{{
				Title:    "Boundary",
				IssueURL: strings.Repeat("x", exactURLLength),
			}},
			check: func(t *testing.T, got string) {
				t.Helper()
				if count := utf8.RuneCountInString(got); count != googleChatMessageRuneBudget {
					t.Fatalf("rune count = %d, want %d", count, googleChatMessageRuneBudget)
				}
				if strings.Contains(got, " more in Multica") {
					t.Fatalf("exactly fitting item was omitted: %q", got)
				}
			},
		},
		{
			name: "one rune over boundary",
			items: []NotificationItem{{
				Title:    "Boundary",
				IssueURL: strings.Repeat("x", exactURLLength+1),
			}},
			check: func(t *testing.T, got string) {
				t.Helper()
				if !strings.HasSuffix(got, "…and 1 more in Multica") {
					t.Fatalf("oversized item was not omitted whole: %q", got)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FormatNotifications("Acme", test.items)
			if count := utf8.RuneCountInString(got); count > googleChatMessageRuneBudget {
				t.Fatalf("rune count = %d, budget = %d", count, googleChatMessageRuneBudget)
			}
			test.check(t, got)
		})
	}
}

func TestFormatConfirmationEscapesWorkspaceName(t *testing.T) {
	t.Parallel()

	got := FormatConfirmation("Research & <Design>")
	if got != "Multica notifications are connected for *Research &amp; &lt;Design&gt;*." {
		t.Fatalf("FormatConfirmation() = %q", got)
	}
}
