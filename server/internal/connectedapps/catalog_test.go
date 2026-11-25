package connectedapps

import "testing"

func TestCuratedAppsHaveNames(t *testing.T) {
	for _, app := range curatedApps {
		if app.Name == "" {
			t.Errorf("curated app %q has no name", app.Slug)
		}
	}
}

func TestAppLabel(t *testing.T) {
	for slug, want := range map[string]string{
		"posthog":        "PostHog",
		"GoogleCalendar": "Google Calendar",
		"hubspot":        "HubSpot",
		"some_app":       "Some App",
	} {
		if got := appLabel(slug); got != want {
			t.Errorf("appLabel(%q) = %q, want %q", slug, got, want)
		}
	}
}
