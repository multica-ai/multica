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
		"-o", "stream-json",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestBuildGeminiArgsWithModel(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{Model: "gemini-2.5-pro"})

	var foundModel bool
	for i, a := range args {
		if a == "-m" {
			if i+1 >= len(args) || args[i+1] != "gemini-2.5-pro" {
				t.Fatalf("expected -m followed by gemini-2.5-pro, got %v", args)
			}
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("expected -m flag when Model is set, got args=%v", args)
	}
}

func TestBuildGeminiArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{ResumeSessionID: "3"})

	var foundResume bool
	for i, a := range args {
		if a == "-r" {
			if i+1 >= len(args) || args[i+1] != "3" {
				t.Fatalf("expected -r followed by session id, got %v", args)
			}
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected -r flag when ResumeSessionID is set, got args=%v", args)
	}
}

func TestBuildGeminiArgsOmitsModelWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{})
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("expected no -m flag when Model is empty, got args=%v", args)
		}
		if a == "-r" {
			t.Fatalf("expected no -r flag when ResumeSessionID is empty, got args=%v", args)
		}
	}
}
