package main

import "testing"

func TestQuietBodyKeepsTheWindowAndAppliesOnlyTheChanges(t *testing.T) {
	current := map[string]any{"enabled": false, "start": "22:00", "end": "07:00", "timezone": "Europe/Paris"}

	onlyEnable := quietBodyFrom(current, quietChange{Enable: true})
	if onlyEnable["enabled"] != true || onlyEnable["start"] != "22:00" || onlyEnable["end"] != "07:00" || onlyEnable["timezone"] != "Europe/Paris" {
		t.Fatalf("enable alone = %+v; the window must keep its times and zone", onlyEnable)
	}

	start, zone := "23:00", "UTC"
	moved := quietBodyFrom(current, quietChange{Start: &start, Timezone: &zone})
	if moved["enabled"] != false || moved["start"] != "23:00" || moved["end"] != "07:00" || moved["timezone"] != "UTC" {
		t.Fatalf("moving the start = %+v", moved)
	}

	off := quietBodyFrom(map[string]any{"enabled": true, "start": "22:00", "end": "07:00", "timezone": "UTC"}, quietChange{Disable: true})
	if off["enabled"] != false {
		t.Fatalf("disable = %+v", off)
	}
}
