package main

import (
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/redact"
)

type stdinCapture struct {
	reporter *localRunReporter
	turns    *terminalTurnCapture
	editor   *terminalInputEditor
}

func (c *stdinCapture) Write(p []byte) (int, error) {
	if c == nil {
		return len(p), nil
	}
	if c.editor == nil {
		c.editor = &terminalInputEditor{}
	}
	for _, content := range c.editor.Write(p) {
		content = redact.Text(strings.TrimSpace(content))
		if content == "" {
			continue
		}
		if c.turns != nil {
			c.turns.BeforeUserSubmit()
		}
		c.reporter.Post(localCLIMessage{Type: "user_input", Content: content})
		if c.turns != nil {
			c.turns.AfterUserSubmit(content)
		}
	}
	return len(p), nil
}

type terminalInputEditor struct {
	buf     []rune
	cursor  int
	state   inputEscapeState
	escBuf  []byte
	utf8Buf []byte
}

type inputEscapeState int

const (
	inputNormal inputEscapeState = iota
	inputEsc
	inputCSI
	inputOSC
	inputOSCEsc
)

func (e *terminalInputEditor) Write(p []byte) []string {
	var commits []string
	for _, b := range p {
		if e.state != inputNormal {
			e.handleEscapeByte(b)
			continue
		}
		switch b {
		case 0x1b:
			e.state = inputEsc
			e.escBuf = e.escBuf[:0]
		case '\r', '\n':
			commits = append(commits, string(e.buf))
			e.buf = e.buf[:0]
			e.cursor = 0
			e.utf8Buf = e.utf8Buf[:0]
		case 0x03, 0x04:
			e.utf8Buf = e.utf8Buf[:0]
		case 0x7f, 0x08:
			e.backspace()
		case 0x15:
			e.buf = e.buf[:0]
			e.cursor = 0
		case 0x17:
			e.ctrlW()
		case 0x01:
			e.cursor = 0
		case 0x05:
			e.cursor = len(e.buf)
		default:
			if b < 0x20 {
				e.utf8Buf = e.utf8Buf[:0]
				continue
			}
			e.writeUTF8Byte(b)
		}
	}
	return commits
}

func (e *terminalInputEditor) handleEscapeByte(b byte) {
	switch e.state {
	case inputEsc:
		switch b {
		case '[':
			e.state = inputCSI
			e.escBuf = e.escBuf[:0]
		case ']':
			e.state = inputOSC
			e.escBuf = e.escBuf[:0]
		default:
			e.state = inputNormal
		}
	case inputCSI:
		if b >= 0x40 && b <= 0x7e {
			e.applyCSI(b, string(e.escBuf))
			e.state = inputNormal
			e.escBuf = e.escBuf[:0]
			return
		}
		e.escBuf = append(e.escBuf, b)
	case inputOSC:
		if b == 0x07 {
			e.state = inputNormal
			return
		}
		if b == 0x1b {
			e.state = inputOSCEsc
		}
	case inputOSCEsc:
		if b == '\\' {
			e.state = inputNormal
		} else {
			e.state = inputOSC
		}
	}
}

func (e *terminalInputEditor) applyCSI(final byte, params string) {
	switch final {
	case 'C':
		if e.cursor < len(e.buf) {
			e.cursor++
		}
	case 'D':
		if e.cursor > 0 {
			e.cursor--
		}
	case '~':
		if strings.HasPrefix(params, "3") && e.cursor < len(e.buf) {
			e.buf = append(e.buf[:e.cursor], e.buf[e.cursor+1:]...)
		}
	}
}

func (e *terminalInputEditor) writeUTF8Byte(b byte) {
	e.utf8Buf = append(e.utf8Buf, b)
	r, size := utf8.DecodeRune(e.utf8Buf)
	if r == utf8.RuneError && size == 1 && !utf8.FullRune(e.utf8Buf) {
		return
	}
	if r == utf8.RuneError && size == 1 {
		e.utf8Buf = e.utf8Buf[:0]
		return
	}
	e.buf = append(e.buf, 0)
	copy(e.buf[e.cursor+1:], e.buf[e.cursor:])
	e.buf[e.cursor] = r
	e.cursor++
	e.utf8Buf = e.utf8Buf[:0]
}

func (e *terminalInputEditor) backspace() {
	if e.cursor == 0 {
		return
	}
	e.buf = append(e.buf[:e.cursor-1], e.buf[e.cursor:]...)
	e.cursor--
}

func (e *terminalInputEditor) ctrlW() {
	for e.cursor > 0 && unicode.IsSpace(e.buf[e.cursor-1]) {
		e.backspace()
	}
	for e.cursor > 0 && !unicode.IsSpace(e.buf[e.cursor-1]) {
		e.backspace()
	}
}

type terminalTurnCapture struct {
	mu        sync.Mutex
	reporter  *localRunReporter
	screen    *terminalScreen
	provider  providerTranscriptExtractor
	active    bool
	baseline  string
	source    assistantCaptureSource
	lastUser  string
	turnStart time.Time
}

type assistantCaptureSource int

const (
	assistantCaptureNone assistantCaptureSource = iota
	assistantCaptureStructured
	assistantCaptureProvider
)

func newTerminalTurnCapture(reporter *localRunReporter, provider providerTranscriptExtractor) *terminalTurnCapture {
	return &terminalTurnCapture{reporter: reporter, screen: newTerminalScreen(), provider: provider}
}

func (c *terminalTurnCapture) Write(p []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.screen.Write(p)
}

func (c *terminalTurnCapture) MarkStructuredAssistant() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		c.source = assistantCaptureStructured
	}
}

func (c *terminalTurnCapture) BeforeUserSubmit() {
	if c != nil {
		c.finalizeLocked()
	}
}

func (c *terminalTurnCapture) AfterUserSubmit(content string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseline = c.screen.Text()
	c.active = true
	c.source = assistantCaptureNone
	c.lastUser = content
	c.turnStart = time.Now()
}

func (c *terminalTurnCapture) Finalize() {
	if c != nil {
		c.finalizeLocked()
	}
}

func (c *terminalTurnCapture) finalizeLocked() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	if c.source == assistantCaptureNone {
		if content := c.extractProviderLocked(); content != "" {
			c.reporter.Post(localCLIMessage{Type: "final", Content: redact.Text(content)})
			c.source = assistantCaptureProvider
		}
	}
	if c.source == assistantCaptureNone {
		if content := extractAssistantCandidate(c.baseline, c.screen.Text(), c.lastUser); content != "" {
			c.reporter.Post(localCLIMessage{Type: "final", Content: redact.Text(content)})
		}
	}
	c.active = false
	c.baseline = ""
	c.source = assistantCaptureNone
	c.lastUser = ""
	c.turnStart = time.Time{}
}

func (c *terminalTurnCapture) extractProviderLocked() string {
	if c.provider == nil || strings.TrimSpace(c.lastUser) == "" {
		return ""
	}
	answer, ok := c.provider.Extract(c.lastUser, c.turnStart)
	if !ok {
		return ""
	}
	answer = strings.TrimSpace(answer)
	if answer == "" || isStatusOnly(answer) {
		return ""
	}
	return answer
}

type terminalScreen struct {
	lines   [][]rune
	row     int
	col     int
	state   screenEscapeState
	csiBuf  []byte
	utf8Buf []byte
	oscEsc  bool
}

type screenEscapeState int

const (
	screenNormal screenEscapeState = iota
	screenEsc
	screenCSI
	screenOSC
)

func newTerminalScreen() *terminalScreen {
	return &terminalScreen{lines: [][]rune{{}}}
}

func (s *terminalScreen) Write(p []byte) {
	for _, b := range p {
		if s.state != screenNormal {
			s.handleEscapeByte(b)
			continue
		}
		switch b {
		case 0x1b:
			s.state = screenEsc
		case '\r':
			s.col = 0
		case '\n':
			s.row++
			s.col = 0
			s.ensureLine()
		case '\b':
			if s.col > 0 {
				s.col--
			}
		case '\t':
			for i := 0; i < 4; i++ {
				s.putRune(' ')
			}
		default:
			if b >= 0x20 {
				s.writeUTF8Byte(b)
			}
		}
	}
}

func (s *terminalScreen) writeUTF8Byte(b byte) {
	s.utf8Buf = append(s.utf8Buf, b)
	r, size := utf8.DecodeRune(s.utf8Buf)
	if r == utf8.RuneError && size == 1 && !utf8.FullRune(s.utf8Buf) {
		return
	}
	if r == utf8.RuneError && size == 1 {
		s.utf8Buf = s.utf8Buf[:0]
		return
	}
	s.putRune(r)
	s.utf8Buf = s.utf8Buf[:0]
}

func (s *terminalScreen) handleEscapeByte(b byte) {
	switch s.state {
	case screenEsc:
		switch b {
		case '[':
			s.state = screenCSI
			s.csiBuf = s.csiBuf[:0]
		case ']':
			s.state = screenOSC
			s.oscEsc = false
		default:
			s.state = screenNormal
		}
	case screenCSI:
		if b >= 0x40 && b <= 0x7e {
			s.applyCSI(b, string(s.csiBuf))
			s.state = screenNormal
			s.csiBuf = s.csiBuf[:0]
			return
		}
		s.csiBuf = append(s.csiBuf, b)
	case screenOSC:
		if b == 0x07 {
			s.state = screenNormal
			return
		}
		if s.oscEsc && b == '\\' {
			s.state = screenNormal
			s.oscEsc = false
			return
		}
		s.oscEsc = b == 0x1b
	}
}

func (s *terminalScreen) applyCSI(final byte, params string) {
	n := firstCSIParam(params, 1)
	switch final {
	case 'A':
		s.row -= n
		if s.row < 0 {
			s.row = 0
		}
	case 'B':
		s.row += n
		s.ensureLine()
	case 'C':
		s.col += n
	case 'D':
		s.col -= n
		if s.col < 0 {
			s.col = 0
		}
	case 'G':
		if n > 0 {
			s.col = n - 1
		}
	case 'H', 'f':
		row, col := csiRowCol(params)
		s.row = row
		s.col = col
		s.ensureLine()
	case 'J':
		if n == 2 || n == 3 {
			s.lines = [][]rune{{}}
			s.row = 0
			s.col = 0
		}
	case 'K':
		s.ensureLine()
		line := s.lines[s.row]
		switch n {
		case 1:
			if s.col < len(line) {
				for i := 0; i <= s.col; i++ {
					line[i] = ' '
				}
			}
		case 2:
			s.lines[s.row] = nil
		default:
			if s.col < len(line) {
				s.lines[s.row] = line[:s.col]
			}
		}
	}
}

func firstCSIParam(params string, def int) int {
	fields := strings.Split(params, ";")
	if len(fields) == 0 || fields[0] == "" || strings.HasPrefix(fields[0], "?") {
		return def
	}
	var n int
	for _, r := range fields[0] {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func csiRowCol(params string) (int, int) {
	fields := strings.Split(params, ";")
	row := 0
	col := 0
	if len(fields) > 0 {
		row = parseCSIIndex(fields[0])
	}
	if len(fields) > 1 {
		col = parseCSIIndex(fields[1])
	}
	return row, col
}

func parseCSIIndex(s string) int {
	if s == "" {
		return 0
	}
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if n <= 1 {
		return 0
	}
	return n - 1
}

func (s *terminalScreen) putRune(r rune) {
	s.ensureLine()
	line := s.lines[s.row]
	for len(line) < s.col {
		line = append(line, ' ')
	}
	if s.col == len(line) {
		line = append(line, r)
	} else {
		line[s.col] = r
	}
	s.lines[s.row] = line
	s.col++
}

func (s *terminalScreen) ensureLine() {
	for len(s.lines) <= s.row {
		s.lines = append(s.lines, nil)
	}
}

func (s *terminalScreen) Text() string {
	lines := make([]string, len(s.lines))
	for i, line := range s.lines {
		lines[i] = strings.TrimRight(string(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractAssistantCandidate(before, after, lastUser string) string {
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)
	if after == "" || after == before {
		return ""
	}
	candidate := after
	if before != "" && strings.HasPrefix(after, before) {
		candidate = strings.TrimSpace(strings.TrimPrefix(after, before))
	}
	lines := filterAssistantLines(strings.Split(candidate, "\n"), lastUser)
	if len(lines) == 0 {
		return ""
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	if out == "" || isStatusOnly(out) {
		return ""
	}
	return out
}

func filterAssistantLines(lines []string, lastUser string) []string {
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == strings.TrimSpace(lastUser) || isStatusOnly(line) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func isStatusOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	lower := strings.ToLower(s)
	statusPrefixes := []string{"thinking", "working", "loading", "running", "processing", "waiting"}
	for _, prefix := range statusPrefixes {
		if strings.HasPrefix(lower, prefix) && len([]rune(s)) <= 40 {
			return true
		}
	}
	if isBareProgress(s) {
		return true
	}
	onlyMarks := true
	for _, r := range s {
		if !strings.ContainsRune(`|/-\.*•·⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ `, r) {
			onlyMarks = false
			break
		}
	}
	return onlyMarks
}

func isBareProgress(s string) bool {
	if !strings.Contains(s, "%") || len([]rune(s)) > 30 {
		return false
	}
	for _, r := range s {
		if unicode.IsDigit(r) || strings.ContainsRune("% .,/[]()=-", r) {
			continue
		}
		return false
	}
	return true
}
