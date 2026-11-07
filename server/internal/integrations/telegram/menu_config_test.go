package telegram

import "testing"

func testMenu() *MenuConfig {
	return &MenuConfig{
		PrivateChatIDs: []int64{111}, RootText: "Choose", BackText: "Back",
		Pages: []MenuPage{{Command: "tools", Title: "Tools", Actions: []MenuAction{
			{ID: "status", Label: "Status", Request: "Show my task status."},
			{ID: "search", Label: "Search", Request: "First ask what I want to search for, then wait."},
		}}},
	}
}

func TestMenuConfigValidation(t *testing.T) {
	if err := ValidateMenuConfig(nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMenuConfig(testMenu()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*MenuConfig){
		func(m *MenuConfig) { m.PrivateChatIDs = []int64{-100} },
		func(m *MenuConfig) { m.Pages[0].Command = "root" },
		func(m *MenuConfig) { m.Pages[0].Command = "clear" },
		func(m *MenuConfig) { m.Pages[0].Command = "bad:command" },
		func(m *MenuConfig) { m.Pages = append(m.Pages, m.Pages[0]) },
		func(m *MenuConfig) { m.Pages[0].Actions[1].ID = "status" },
		func(m *MenuConfig) { m.Pages[0].Actions[0].Request = " " },
	} {
		m := testMenu()
		change(m)
		if ValidateMenuConfig(m) == nil {
			t.Errorf("accepted invalid menu: %+v", m)
		}
	}
}
