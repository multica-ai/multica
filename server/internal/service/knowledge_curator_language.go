package service

import (
	"strings"
	"unicode"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	curatorOutputLanguageChinese  = "Chinese"
	curatorOutputLanguageEnglish  = "English"
	curatorOutputLanguageFallback = "same language as the issue evidence"
)

func inferCuratorOutputLanguage(bundle CuratorSourceBundle) string {
	return inferCuratorOutputLanguageFromEvidence(curatorLanguageEvidence{
		Title:       bundle.Issue.Title,
		Description: issueDescriptionText(bundle.Issue),
		Comments:    commentLanguageEvidence(bundle.Comments),
		Tasks:       taskLanguageEvidence(bundle.AgentTasks),
	})
}

type curatorLanguageEvidence struct {
	Title       string
	Description string
	Comments    []string
	Tasks       []string
}

func inferCuratorOutputLanguageFromEvidence(evidence curatorLanguageEvidence) string {
	var chineseScore float64
	var englishScore float64
	add := func(text string, weight float64) {
		cjk, latin := curatorLanguageSignals(text)
		chineseScore += float64(cjk) * weight
		englishScore += float64(latin) * weight
	}

	add(evidence.Title, 5)
	add(evidence.Description, 4)
	for _, comment := range evidence.Comments {
		add(comment, 1)
	}
	for _, task := range evidence.Tasks {
		add(task, 0.5)
	}

	if chineseScore >= 2 && chineseScore >= englishScore*0.35 {
		return curatorOutputLanguageChinese
	}
	if englishScore >= 6 && englishScore > chineseScore*2 {
		return curatorOutputLanguageEnglish
	}
	return curatorOutputLanguageFallback
}

func curatorLanguageSignals(text string) (int, int) {
	cjk := 0
	latinWords := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineWeight := 1
		if looksLikeTechnicalEvidenceLine(line) {
			lineWeight = 0
		}
		lineCJK := countCJK(line)
		cjk += lineCJK
		if lineWeight == 0 {
			continue
		}
		latinWords += countLatinWords(line)
	}
	return cjk, latinWords
}

func issueDescriptionText(issue db.Issue) string {
	if issue.Description.Valid {
		return issue.Description.String
	}
	return ""
}

func commentLanguageEvidence(comments []db.Comment) []string {
	values := make([]string, 0, len(comments))
	for _, comment := range comments {
		values = append(values, comment.Content)
	}
	return values
}

func taskLanguageEvidence(tasks []db.AgentTaskQueue) []string {
	values := make([]string, 0, len(tasks))
	for _, task := range tasks {
		values = append(values, taskText(task))
	}
	return values
}

func countCJK(text string) int {
	count := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			count++
		}
	}
	return count
}

func countLatinWords(text string) int {
	words := 0
	inWord := false
	wordLen := 0
	for _, r := range text {
		if unicode.IsLetter(r) && r <= unicode.MaxLatin1 {
			inWord = true
			wordLen++
			continue
		}
		if inWord && wordLen >= 2 {
			words++
		}
		inWord = false
		wordLen = 0
	}
	if inWord && wordLen >= 2 {
		words++
	}
	return words
}

func looksLikeTechnicalEvidenceLine(line string) bool {
	lower := strings.ToLower(line)
	markers := []string{
		"error:", "exception", "panic:", "stack trace", "traceback", " at ",
		"http://", "https://", ".go:", ".ts:", ".tsx:", ".js:", ".jsx:",
		"pnpm ", "npm ", "yarn ", "go test", "curl ", "docker ", "kubectl ",
		"->", "=>", "==", ":=", "=", "/", "\\", "`",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeCuratorOutputLanguage(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return curatorOutputLanguageFallback
	}
	return language
}

func curatorOutputLanguageInstruction(language string) string {
	return strings.Join([]string{
		"Output language: " + normalizeCuratorOutputLanguage(language) + ".",
		"Use the output language for all human-readable JSON text fields.",
		"Keep enum values such as type and confidence_status in English.",
		"Preserve code, commands, error messages, API fields, file paths, identifiers, and proper nouns verbatim instead of translating them.",
	}, " ")
}
