package main

import "testing"

func TestRoundCreateOnlyAcceptsName(t *testing.T) {
	if roundCreateCmd.Flags().Lookup("schedule-cron") != nil || roundCreateCmd.Flags().Lookup("timezone") != nil {
		t.Fatal("round create still exposes schedule flags")
	}
}
