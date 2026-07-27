package telegram

import "testing"

func TestChunkMessage(t *testing.T) {
	got := chunkMessage("abcdef", 4)
	if len(got) != 2 || got[0] != "abcd" || got[1] != "ef" {
		t.Fatalf("unexpected chunks %#v", got)
	}
	if one := chunkMessage("short", 100); len(one) != 1 || one[0] != "short" {
		t.Fatalf("expected single chunk, got %#v", one)
	}
}

func TestTypeTelegramValue(t *testing.T) {
	if string(TypeTelegram) != "telegram" {
		t.Fatalf("TypeTelegram = %q, want telegram", TypeTelegram)
	}
}
