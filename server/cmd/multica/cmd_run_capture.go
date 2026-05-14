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
		content, _, ok := sanitizeTerminalUserInput(content)
		if !ok {
			continue
		}
		content = redact.Text(content)
		if c.turns != nil {
			c.turns.BeforeUserSubmit()
		}
		if c.turns != nil {
			c.turns.AfterUserSubmit(content)
		} else {
			c.reporter.Post(localCLIMessage{Type: "user_input", Content: content, Input: commandInputMetadata(isSlashInput(content))})
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
	mu                  sync.Mutex
	stopOnce            sync.Once
	reporter            *localRunReporter
	screen              *terminalScreen
	provider            providerTranscriptExtractor
	done                chan struct{}
	syncedUsers         map[string]bool
	syncedFinals        map[string]bool
	syncedEvents        map[string]bool
	active              bool
	source              assistantCaptureSource
	suppress            bool
	bootstrap           bool
	bootstrapPrompt     string
	providerCommentable bool
}

type assistantCaptureSource int

const (
	assistantCaptureNone assistantCaptureSource = iota
	assistantCaptureStructured
)

func newTerminalTurnCapture(reporter *localRunReporter, provider providerTranscriptExtractor) *terminalTurnCapture {
	return newTerminalTurnCaptureWithPollInterval(reporter, provider, 500*time.Millisecond)
}

func newTerminalTurnCaptureWithPollInterval(reporter *localRunReporter, provider providerTranscriptExtractor, interval time.Duration) *terminalTurnCapture {
	c := &terminalTurnCapture{reporter: reporter, screen: newTerminalScreen(), provider: provider, done: make(chan struct{})}
	if provider != nil && interval > 0 {
		go c.pollProvider(interval)
	}
	return c
}

func (c *terminalTurnCapture) Write(p []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.screen.Write(p)
}

func (c *terminalTurnCapture) HasProviderTranscript() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	provider := c.provider
	c.mu.Unlock()
	if provider == nil {
		return false
	}
	if extractor, ok := provider.(providerTranscriptEventExtractor); ok {
		_, ok := extractor.ExtractEvents()
		return ok
	}
	_, ok := provider.ExtractTurns()
	return ok
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

func (c *terminalTurnCapture) SuppressStructuredAssistant() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active && c.suppress
}

func (c *terminalTurnCapture) PrepareStructuredFinal() bool {
	if c == nil {
		return true
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return true
	}
	if c.bootstrap {
		c.source = assistantCaptureStructured
		return false
	}
	if c.suppress {
		c.source = assistantCaptureStructured
		return false
	}
	c.source = assistantCaptureStructured
	return true
}

func (c *terminalTurnCapture) BeforeUserSubmit() {
	if c != nil {
		c.finalize()
	}
}

func (c *terminalTurnCapture) StartInitialPrompt(content string) {
	if c == nil {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = true
	c.source = assistantCaptureNone
	c.suppress = false
	c.bootstrap = true
	c.bootstrapPrompt = content
}

func (c *terminalTurnCapture) AfterUserSubmit(content string) {
	if c == nil {
		return
	}
	content, command, ok := sanitizeTerminalUserInput(content)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = true
	c.source = assistantCaptureNone
	c.suppress = false
	c.bootstrap = false
	if command {
		c.syncStdinUserInputLocked(content, true)
	}
}

func (c *terminalTurnCapture) Finalize() {
	if c == nil {
		return
	}
	c.finalize()
	c.stopOnce.Do(func() {
		close(c.done)
	})
}

func (c *terminalTurnCapture) finalize() {
	if c.shouldSyncProviderTurns() {
		c.syncProviderTurns()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	c.active = false
	c.source = assistantCaptureNone
	c.suppress = false
	c.bootstrap = false
}

func (c *terminalTurnCapture) pollProvider(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if c.shouldSyncProviderTurns() {
				c.syncProviderTurns()
			}
		case <-c.done:
			return
		}
	}
}

func (c *terminalTurnCapture) shouldSyncProviderTurns() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !(c.active && c.source == assistantCaptureStructured)
}

func (c *terminalTurnCapture) syncProviderTurns() {
	if c == nil || c.provider == nil {
		return
	}
	if extractor, ok := c.provider.(providerTranscriptEventExtractor); ok {
		events, ok := extractor.ExtractEvents()
		if !ok {
			return
		}
		for _, event := range events {
			c.syncProviderEvent(event)
		}
		return
	}
	turns, ok := c.provider.ExtractTurns()
	if !ok {
		return
	}
	for _, turn := range turns {
		user := strings.TrimSpace(turn.UserInput)
		if user == "" || c.isBootstrapProviderUserInput(user) || isSlashInput(user) {
			continue
		}
		c.syncProviderUser(turn.Key, user)
		final := strings.TrimSpace(turn.Final)
		if final == "" || isStatusOnly(final) {
			continue
		}
		c.syncProviderFinal(turn.Key, final)
	}
}

func (c *terminalTurnCapture) syncProviderEvent(event providerTranscriptEvent) {
	if event.Key == "" || strings.TrimSpace(event.Type) == "" {
		return
	}
	msg := localCLIMessage{
		Type:      event.Type,
		Tool:      event.Tool,
		Content:   strings.TrimSpace(event.Content),
		Input:     event.Input,
		Output:    strings.TrimSpace(event.Output),
		Source:    event.Source,
		SourceKey: event.Key,
	}
	if msg.Source == "" {
		msg.Source = "provider"
	}
	if msg.Type == "user_input" {
		if msg.Content == "" || c.isBootstrapProviderUserInput(msg.Content) || isSlashInput(msg.Content) {
			c.setProviderCommentable(false)
			return
		}
		c.setProviderCommentable(true)
	}
	if msg.Type == "final" {
		if msg.Content == "" || isStatusOnly(msg.Content) || !event.Comment || !c.isProviderCommentable() {
			return
		}
	}
	if msg.Type != "user_input" && msg.Type != "final" && msg.Content == "" && msg.Output == "" && msg.Tool == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureProviderSyncMapsLocked()
	if c.syncedEvents[event.Key] {
		return
	}
	msg.Content = redact.Text(msg.Content)
	msg.Output = redact.Text(msg.Output)
	msg.Input = redactInputMap(msg.Input)
	c.reporter.Post(msg)
	c.syncedEvents[event.Key] = true
}

func (c *terminalTurnCapture) setProviderCommentable(commentable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providerCommentable = commentable
}

func (c *terminalTurnCapture) isProviderCommentable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.providerCommentable
}

func (c *terminalTurnCapture) syncProviderUser(key, content string) {
	if key == "" || strings.TrimSpace(content) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureProviderSyncMapsLocked()
	if c.syncedUsers[key] {
		return
	}
	c.reporter.Post(localCLIMessage{Type: "user_input", Content: redact.Text(content)})
	c.syncedUsers[key] = true
}

func (c *terminalTurnCapture) syncProviderFinal(key, content string) {
	if key == "" || strings.TrimSpace(content) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureProviderSyncMapsLocked()
	if c.syncedFinals[key] {
		return
	}
	c.reporter.Post(localCLIMessage{Type: "final", Content: redact.Text(content)})
	c.syncedFinals[key] = true
}

func (c *terminalTurnCapture) ensureProviderSyncMapsLocked() {
	if c.syncedUsers == nil {
		c.syncedUsers = make(map[string]bool)
	}
	if c.syncedFinals == nil {
		c.syncedFinals = make(map[string]bool)
	}
	if c.syncedEvents == nil {
		c.syncedEvents = make(map[string]bool)
	}
}

func (c *terminalTurnCapture) isBootstrapProviderUserInput(content string) bool {
	c.mu.Lock()
	bootstrapPrompt := c.bootstrapPrompt
	c.mu.Unlock()
	if strings.TrimSpace(bootstrapPrompt) == "" {
		return false
	}
	content = normalizeProviderText(content)
	bootstrap := normalizeProviderText(bootstrapPrompt)
	if content == "" || bootstrap == "" {
		return false
	}
	if content == bootstrap {
		return true
	}
	return strings.Contains(content, "You are assigned to Multica issue") &&
		strings.Contains(content, "Assigned issue ID:")
}

func (c *terminalTurnCapture) syncStdinUserInputLocked(content string, command bool) {
	if strings.TrimSpace(content) == "" {
		return
	}
	c.reporter.Post(localCLIMessage{Type: "user_input", Content: redact.Text(content), Input: commandInputMetadata(command)})
	c.suppress = command
}

func sanitizeTerminalUserInput(content string) (string, bool, bool) {
	content = normalizeCapturedUserText(strings.TrimSpace(content))
	if content == "" || isRejectedTerminalUserInput(content) {
		return "", false, false
	}
	return content, isSlashInput(content), true
}

func normalizeCapturedUserText(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" {
		return ""
	}
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == ' ' && i > 0 && i+1 < len(runes) && isHan(runes[i-1]) && isHan(runes[i+1]) {
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func isRejectedTerminalUserInput(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(strings.TrimLeft(s, "✓✔•└─ ")))
	rejectedPrefixes := []string{
		"--output json",
		"status, add comments",
		"only. do not",
		"ready, and",
		"you approved codex to run ",
		"codex wants to run ",
		"allow command?",
		"approval required",
		"model:",
		"directory:",
		"cwd:",
		"explored",
		"read ",
		"listed ",
	}
	for _, prefix := range rejectedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isSlashInput(content string) bool {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	switch name {
	case "approvals", "clear", "compact", "diff", "exit", "help", "init", "mcp", "model", "new", "prompts", "quit", "resume", "review", "settings", "status":
		return true
	default:
		return false
	}
}

func commandInputMetadata(command bool) map[string]any {
	if !command {
		return nil
	}
	return map[string]any{"command": true}
}

func looksLikePathToken(s string) bool {
	s = strings.Trim(strings.TrimSpace(s), "`'\"")
	if s == "" || strings.HasSuffix(s, "/") {
		return false
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "/") {
		return strings.Contains(s, "/") && pathTokenHasFileName(s)
	}
	return strings.Contains(s, "/") && pathTokenHasFileName(s)
}

func pathTokenHasFileName(s string) bool {
	base := s
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.Contains(base, ".")
}

func isHan(r rune) bool {
	return unicode.Is(unicode.Han, r)
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
	switch final {
	case 'A':
		n := firstCSIParam(params, 1)
		s.row -= n
		if s.row < 0 {
			s.row = 0
		}
	case 'B':
		n := firstCSIParam(params, 1)
		s.row += n
		s.ensureLine()
	case 'C':
		n := firstCSIParam(params, 1)
		s.col += n
	case 'D':
		n := firstCSIParam(params, 1)
		s.col -= n
		if s.col < 0 {
			s.col = 0
		}
	case 'G':
		n := firstCSIParam(params, 1)
		if n > 0 {
			s.col = n - 1
		}
	case 'H', 'f':
		row, col := csiRowCol(params)
		s.row = row
		s.col = col
		s.ensureLine()
	case 'J':
		n := firstCSIParam(params, 0)
		if n == 2 || n == 3 {
			s.lines = [][]rune{{}}
			s.row = 0
			s.col = 0
		}
	case 'K':
		n := firstCSIParam(params, 0)
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

func isStatusOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(strings.TrimLeft(s, "✓✔•└─>› ")))
	statusPrefixes := []string{"think", "thinking", "work", "working", "loading", "running", "processing", "waiting"}
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
