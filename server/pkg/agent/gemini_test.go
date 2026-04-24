package agent

import (
	"testing"
)

func TestBuildGeminiArgsBaseline(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("write a haiku", ExecOptions{})
	expected := []string{
		"-p", "write a haiku",
		"--yolo",
		"-o", "text",
	}
	if len(args) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Fatalf("at index %d: expected %q, got %q (full: %v)", i, expected[i], a, args)
		}
	}
}

func TestBuildGeminiArgsWithModel(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{Model: "gemini-2.5-pro"})

	var foundModel bool
	for i, a := range args {
		if a == "-m" && i+1 < len(args) && args[i+1] == "gemini-2.5-pro" {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("expected -m gemini-2.5-pro in args, got %v", args)
	}
}

func TestBuildGeminiArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{ResumeSessionID: "3"})

	var foundResume bool
	for i, a := range args {
		if a == "-r" && i+1 < len(args) && args[i+1] == "3" {
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected -r 3 in args, got %v", args)
	}
}

func TestBuildGeminiArgsOmitsModelWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{})
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("expected no -m flag when Model is empty, got args=%v", args)
		}
	}
}
