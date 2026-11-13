package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"phoenix/internal/config"
	"phoenix/internal/desktop"
	"phoenix/internal/provider"
)

const (
	chatStoreFileMaxBytes = 96 << 20
	chatStoreMaxChats     = 40
	chatStoreMaxMessages  = 400
	chatStoreMaxChars     = 1_200_000 // a full 256k-token session (content and context are both counted)
	chatStoreMaxMessage   = 50_000
	chatStoreTitleRunes   = 96
	activityStoreMaxItems = 500
	activityDisplayItems  = 100
	activitySummaryRunes  = 120
)

type chatTranscriptMessage struct {
	Role        string   `json:"role"`
	Content     string   `json:"content"`
	At          int64    `json:"at,omitempty"`
	Context     string   `json:"context,omitempty"`
	InContext   bool     `json:"inContext,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Model       string   `json:"model,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

type chatDisplayMessage struct {
	Role        string   `json:"role"`
	Content     string   `json:"content"`
	At          int64    `json:"at,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Model       string   `json:"model,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

type savedChat struct {
	ID        string                  `json:"id"`
	Title     string                  `json:"title"`
	CreatedAt time.Time               `json:"createdAt"`
	UpdatedAt time.Time               `json:"updatedAt"`
	Messages  []chatTranscriptMessage `json:"messages"`
	Pending   *pendingChatRun         `json:"pending,omitempty"`
}

// pendingChatRun stores only enough to offer a fresh attempt after a process interruption.
// Tool arguments, results and approval decisions are deliberately not persisted here.
type pendingChatRun struct {
	Request  string `json:"request"`
	LastTool string `json:"lastTool,omitempty"`
	TaskID   string `json:"taskId,omitempty"`
}

type savedChatSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
	Messages  int       `json:"messages"`
}

// savedChatList is what the panel receives: the sessions, newest first, and which one is open.
type savedChatList struct {
	Current string             `json:"current"`
	Items   []savedChatSummary `json:"items"`
}

type agentActivity struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
	Status  string    `json:"status,omitempty"`
	Millis  int64     `json:"ms,omitempty"`
}

type agentDataWrite struct {
	path   string
	value  any
	remove bool
}

var (
	savedChats        []savedChat
	agentActivities   []agentActivity
	chatStorePath     string
	activityStorePath string
	agentWriteOnce    sync.Once
	agentWriteWake    = make(chan struct{}, 1)
	agentWriteIO      sync.Mutex
	agentWriteQueue   = struct {
		sync.Mutex
		pending map[string]agentDataWrite
	}{pending: make(map[string]agentDataWrite)}
)

func initAgentPersistence() {
	agentWriteOnce.Do(func() { go runAgentDataWriter() })
	var dir string
	if profile := os.Getenv("PHOENIX_TEST_PROFILE"); profile != "" {
		dir = profile
	} else if configPath, err := config.DefaultPath(); err == nil {
		dir = filepath.Dir(configPath)
	} else {
		logf("agent data path: %v", err)
		return
	}
	chatStorePath = filepath.Join(dir, "chats.json")
	activityStorePath = filepath.Join(dir, "agent-activity.json")
	loadSavedChats()
	loadAgentActivities()
}

func loadSavedChats() {
	data, ok := readAgentData(chatStorePath)
	if !ok {
		return
	}
	var chats []savedChat
	if err := json.Unmarshal(data, &chats); err != nil {
		logf("decode saved chats: %v", err)
		return
	}
	for _, c := range chats {
		if cleanSavedChat(&c) {
			savedChats = append(savedChats, c)
			if len(savedChats) == chatStoreMaxChats {
				break
			}
		}
	}
}

func loadAgentActivities() {
	data, ok := readAgentData(activityStorePath)
	if !ok {
		return
	}
	var entries []agentActivity
	if err := json.Unmarshal(data, &entries); err != nil {
		logf("decode agent activity: %v", err)
		return
	}
	for _, entry := range entries {
		if cleanActivity(&entry) {
			agentActivities = append(agentActivities, entry)
			if len(agentActivities) == activityStoreMaxItems {
				break
			}
		}
	}
}

func readAgentData(path string) ([]byte, bool) {
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		logf("inspect agent data: %v", err)
		return nil, false
	}
	if info.Size() > chatStoreFileMaxBytes {
		logf("ignore oversized agent data file (%d bytes)", info.Size())
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		logf("read agent data: %v", err)
		return nil, false
	}
	return b, true
}

func cleanSavedChat(c *savedChat) bool {
	if !validSavedChatID(c.ID) {
		return false
	}
	c.Title = clippedText(c.Title, chatStoreTitleRunes)
	if c.Title == "" {
		c.Title = "Saved chat"
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	if c.UpdatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		c.UpdatedAt = c.CreatedAt
	}
	if len(c.Messages) > chatStoreMaxMessages {
		c.Messages = append([]chatTranscriptMessage(nil), c.Messages[len(c.Messages)-chatStoreMaxMessages:]...)
	}
	clean := make([]chatTranscriptMessage, 0, len(c.Messages))
	total := 0
	for _, m := range c.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		m.Content = clippedUTF8(m.Content, chatStoreMaxMessage)
		m.Context = clippedUTF8(m.Context, chatStoreMaxMessage)
		m.Provider = clippedText(m.Provider, 48)
		m.Model = clippedText(m.Model, 96)
		if m.Role == "assistant" && len(m.Suggestions) == 2 {
			m.Suggestions = []string{clippedUTF8(m.Suggestions[0], 512), clippedUTF8(m.Suggestions[1], 512)}
			b, _ := json.Marshal(m.Suggestions)
			m.Suggestions = suggestionOptions(string(b))
		} else {
			m.Suggestions = nil
		}
		if m.Content == "" {
			continue
		}
		remaining := chatStoreMaxChars - total
		if remaining <= 0 {
			break
		}
		if len(m.Content) > remaining {
			m.Content = clippedUTF8(m.Content, remaining)
		}
		remaining -= len(m.Content)
		if len(m.Context) > remaining {
			m.Context = clippedUTF8(m.Context, remaining)
		}
		total += len(m.Content) + len(m.Context)
		clean = append(clean, m)
	}
	c.Messages = clean
	if c.Pending != nil {
		c.Pending.Request = clippedUTF8(c.Pending.Request, chatMaxInput)
		c.Pending.LastTool = clippedText(c.Pending.LastTool, activitySummaryRunes)
		c.Pending.TaskID = clippedText(c.Pending.TaskID, 128)
		if strings.TrimSpace(c.Pending.Request) == "" {
			c.Pending = nil
		}
	}
	return true
}

func setPendingChatRun(id, request, taskID string) {
	if c := findSavedChat(id); c != nil {
		c.Pending = &pendingChatRun{Request: clippedUTF8(request, chatMaxInput), TaskID: taskID}
		persistSavedChats()
		flushAgentDataWrites()
	}
}

func interruptedTaskChat(taskID string) string {
	for _, c := range savedChats {
		if c.Pending != nil && c.Pending.TaskID == taskID && taskID != "" {
			return c.ID
		}
	}
	return ""
}

func clearInterruptedTask(taskID string) {
	for i := range savedChats {
		if savedChats[i].Pending != nil && savedChats[i].Pending.TaskID == taskID {
			savedChats[i].Pending = nil
		}
	}
	persistSavedChats()
}

func checkpointChatTool(id, tool string) {
	if c := findSavedChat(id); c != nil && c.Pending != nil {
		c.Pending.LastTool = clippedText(tool, activitySummaryRunes)
		persistSavedChats()
	}
}

func clearPendingChatRun(id string) {
	if c := findSavedChat(id); c != nil && c.Pending != nil {
		c.Pending = nil
		persistSavedChats()
	}
}

func validSavedChatID(id string) bool {
	part, ok := strings.CutPrefix(id, "chat-")
	if !ok || len(part) < 1 || len(part) > 32 {
		return false
	}
	for _, r := range part {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func cleanActivity(a *agentActivity) bool {
	if a.At.IsZero() || a.At.After(time.Now().Add(24*time.Hour)) {
		return false
	}
	switch a.Kind {
	case "tool", "approval", "turn":
	default:
		return false
	}
	a.Summary = clippedText(a.Summary, activitySummaryRunes)
	if a.Summary == "" {
		return false
	}
	switch a.Status {
	case "", "started", "completed", "stopped", "failed", "pending", "allowed", "declined":
	default:
		a.Status = ""
	}
	if a.Millis < 0 || a.Millis > int64((10*time.Minute)/time.Millisecond) {
		a.Millis = 0
	}
	return true
}

func newSavedChat() savedChat {
	now := time.Now()
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return savedChat{ID: "chat-" + strconv.FormatInt(now.UnixNano(), 16), CreatedAt: now, UpdatedAt: now}
	}
	return savedChat{ID: "chat-" + hex.EncodeToString(idBytes[:]), CreatedAt: now, UpdatedAt: now}
}

func saveUserMessage(chatID string, display, contextText string) (string, int, int64) {
	promoteSavedChat(chatID)
	c := findSavedChat(chatID)
	if c == nil {
		created := newSavedChat()
		savedChats = append([]savedChat{created}, savedChats...)
		c = &savedChats[0]
	}
	if len(c.Messages) >= chatStoreMaxMessages {
		c.Messages = append([]chatTranscriptMessage(nil), c.Messages[1:]...)
	}
	content := clippedUTF8(strings.ToValidUTF8(display, "�"), chatStoreMaxMessage)
	contextText = clippedUTF8(strings.ToValidUTF8(contextText, "�"), chatStoreMaxMessage)
	if c.Title == "" || c.Title == "Saved chat" {
		c.Title = clippedText(content, chatStoreTitleRunes)
	}
	m := chatTranscriptMessage{Role: "user", Content: content, At: time.Now().UnixMilli()}
	if contextText != content {
		m.Context = contextText
	}
	c.Messages = append(c.Messages, m)
	c.UpdatedAt = time.Now()
	id := c.ID
	trimSavedChats()
	index := len(c.Messages) - 1
	persistSavedChats()
	pushSavedChatsFor(id)
	return id, index, m.At
}

func commitSavedTurn(chatID string, userIndex int, answer, providerName, model string) int64 {
	c := findSavedChat(chatID)
	if c == nil || userIndex < 0 || userIndex >= len(c.Messages) || c.Messages[userIndex].Role != "user" {
		return 0
	}
	u := &c.Messages[userIndex]
	u.InContext = true
	if u.Context == "" {
		u.Context = u.Content
	}
	answer = clippedUTF8(strings.ToValidUTF8(answer, "�"), chatStoreMaxMessage)
	at := time.Now().UnixMilli()
	if answer != "" {
		c.Messages = append(c.Messages, chatTranscriptMessage{
			Role: "assistant", Content: answer, InContext: true, At: at,
			Provider: clippedText(providerName, 48), Model: clippedText(model, 96),
		})
	}
	trimChatMessages(c)
	c.UpdatedAt = time.Now()
	persistSavedChats()
	pushSavedChats()
	return at
}

func restoreSavedChat(id string) (*savedChat, bool) {
	c := findSavedChat(id)
	if c == nil {
		return nil, false
	}
	return c, true
}

func findSavedChat(id string) *savedChat {
	for i := range savedChats {
		if savedChats[i].ID == id {
			return &savedChats[i]
		}
	}
	return nil
}

func promoteSavedChat(id string) {
	for i := range savedChats {
		if savedChats[i].ID != id {
			continue
		}
		if i > 0 {
			c := savedChats[i]
			copy(savedChats[1:i+1], savedChats[:i])
			savedChats[0] = c
		}
		return
	}
}

func trimSavedChats() {
	for i := range savedChats {
		trimChatMessages(&savedChats[i])
	}
	if len(savedChats) > chatStoreMaxChats {
		savedChats = savedChats[:chatStoreMaxChats]
	}
}

func trimChatMessages(c *savedChat) {
	for len(c.Messages) > chatStoreMaxMessages {
		c.Messages = c.Messages[1:]
	}
	total := 0
	for _, m := range c.Messages {
		total += len(m.Content) + len(m.Context)
	}
	for total > chatStoreMaxChars && len(c.Messages) > 1 {
		first := c.Messages[0]
		total -= len(first.Content) + len(first.Context)
		c.Messages = c.Messages[1:]
	}
}

func addAgentActivity(kind, summary, status string, millis int64) {
	a := agentActivity{
		At: time.Now(), Kind: kind, Summary: clippedText(summary, activitySummaryRunes),
		Status: status, Millis: millis,
	}
	if !cleanActivity(&a) {
		return
	}
	agentActivities = append([]agentActivity{a}, agentActivities...)
	if len(agentActivities) > activityStoreMaxItems {
		agentActivities = agentActivities[:activityStoreMaxItems]
	}
	persistAgentActivities()
	pushAgentActivity()
}

func safeToolSummary(name string) string {
	// Tool identifiers are useful activity labels; provider arguments and results are never
	// passed here. Desktop app names and approval summaries are intentionally excluded.
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "Tool used"
	}
	server, tool, ok := strings.Cut(rest, "__")
	if !ok {
		return "Tool used"
	}
	server, tool = safeIdentifier(server), safeIdentifier(tool)
	if server == "" || tool == "" {
		return "Tool used"
	}
	switch server {
	case "phoenix-browser":
		return "Browser · " + strings.TrimPrefix(tool, "browser_")
	case "phoenix-desktop":
		for _, skill := range desktop.Skills {
			if skill.Tool == tool {
				return "Computer · " + skill.Label
			}
		}
		return "Computer · tool"
	default:
		return server + " · " + tool
	}
}

func safeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			if b.Len() >= 48 {
				break
			}
		}
	}
	return b.String()
}

func clearAgentPersistence() {
	savedChats = nil
	agentActivities = nil
	removeAgentData(chatStorePath)
	removeAgentData(activityStorePath)
	flushAgentDataWrites()
	pushSavedChats()
	pushAgentActivity()
}

func removeAgentData(path string) {
	if path == "" {
		return
	}
	queueAgentDataWrite(agentDataWrite{path: path, remove: true})
}

func pushSavedChats() { pushSavedChatsFor(chat.currentID) }

// pushSavedChatsFor sends the session list with `current` marked as the open one. A new session
// gets its id only after saveUserMessage returns, so that caller names it explicitly.
func pushSavedChatsFor(current string) {
	if !ready {
		return
	}
	sort.SliceStable(savedChats, func(i, j int) bool {
		return savedChats[i].UpdatedAt.After(savedChats[j].UpdatedAt)
	})
	items := make([]savedChatSummary, 0, len(savedChats))
	for _, c := range savedChats {
		items = append(items, savedChatSummary{ID: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt, Messages: len(c.Messages)})
	}
	post("u" + fs + "chat" + fs + "chats" + fs + jsonString(savedChatList{Current: current, Items: items}))
}

func pushAgentActivity() {
	if !ready {
		return
	}
	items := make([]agentActivity, len(agentActivities))
	if len(items) > activityDisplayItems {
		items = make([]agentActivity, activityDisplayItems)
	}
	copy(items, agentActivities[:len(items)])
	post("u" + fs + "agent-activity" + fs + "activity" + fs + jsonString(items))
}

func restoreLatestSavedChat() {
	if len(savedChats) == 0 {
		return
	}
	activateSavedChat(&savedChats[0])
}

func activateSavedChat(c *savedChat) {
	if c == nil {
		return
	}
	chat.currentID = c.ID
	chat.pendingMessageIndex = -1
	chat.history = nil
	pushSavedChats()
	display := make([]chatDisplayMessage, 0, len(c.Messages))
	for _, m := range c.Messages {
		if m.Role == "user" || m.Role == "assistant" {
			display = append(display, chatDisplayMessage{Role: m.Role, Content: m.Content, At: m.At, Provider: m.Provider, Model: m.Model, Suggestions: m.Suggestions})
		}
		if !m.InContext || (m.Role != "user" && m.Role != "assistant") {
			continue
		}
		content := m.Context
		if content == "" {
			content = m.Content
		}
		chat.history = append(chat.history, provider.Message{Role: m.Role, Content: content})
	}
	postChat(chatEvent{K: "restore", Messages: display})
	if c.Pending != nil {
		note := "This run was interrupted. Its last action may have completed."
		if c.Pending.LastTool != "" {
			note += " Last tool: " + c.Pending.LastTool + "."
		}
		if c.Pending.TaskID != "" {
			note += " Review the task and its results before running it again."
		} else {
			note += " Continue to inspect the current state before taking further action."
		}
		postChat(chatEvent{K: "interrupted", Text: note, Resume: c.Pending.TaskID == ""})
	}
	pushContext()
}

func persistSavedChats() {
	if chatStorePath == "" {
		return
	}
	trimSavedChats()
	snapshot := make([]savedChat, len(savedChats))
	copy(snapshot, savedChats)
	for i := range snapshot {
		snapshot[i].Messages = append([]chatTranscriptMessage(nil), snapshot[i].Messages...)
		if snapshot[i].Pending != nil {
			pending := *snapshot[i].Pending
			snapshot[i].Pending = &pending
		}
	}
	queueAgentDataWrite(agentDataWrite{path: chatStorePath, value: snapshot})
}

func persistAgentActivities() {
	if activityStorePath == "" {
		return
	}
	snapshot := append([]agentActivity(nil), agentActivities...)
	queueAgentDataWrite(agentDataWrite{path: activityStorePath, value: snapshot})
}

func queueAgentDataWrite(job agentDataWrite) {
	agentWriteQueue.Lock()
	agentWriteQueue.pending[job.path] = job
	agentWriteQueue.Unlock()
	select {
	case agentWriteWake <- struct{}{}:
	default:
	}
}

func runAgentDataWriter() {
	for range agentWriteWake {
		for {
			agentWriteIO.Lock()
			agentWriteQueue.Lock()
			var job agentDataWrite
			for path, pending := range agentWriteQueue.pending {
				job = pending
				delete(agentWriteQueue.pending, path)
				break
			}
			agentWriteQueue.Unlock()
			if job.path == "" {
				agentWriteIO.Unlock()
				break
			}
			processAgentDataWrite(job)
			agentWriteIO.Unlock()
		}
	}
}

func flushAgentDataWrites() {
	agentWriteIO.Lock()
	defer agentWriteIO.Unlock()
	for {
		agentWriteQueue.Lock()
		var job agentDataWrite
		for path, pending := range agentWriteQueue.pending {
			job = pending
			delete(agentWriteQueue.pending, path)
			break
		}
		agentWriteQueue.Unlock()
		if job.path == "" {
			return
		}
		processAgentDataWrite(job)
	}
}

func processAgentDataWrite(job agentDataWrite) {
	if job.remove {
		if err := os.Remove(job.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			logf("clear agent data: %v", err)
		}
		return
	}
	writeAgentDataNow(job.path, job.value)
}

func writeAgentDataNow(path string, value any) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		logf("create agent data folder: %v", err)
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		logf("encode agent data: %v", err)
		return
	}
	if len(data) > chatStoreFileMaxBytes {
		logf("agent data exceeds size limit: %d bytes", len(data))
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		logf("write agent data: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		logf("save agent data: %v", err)
	}
}

func clippedText(s string, maxRunes int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, "�"))
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes])
	}
	return s
}

func clippedUTF8(s string, maxBytes int) string {
	if maxBytes < 0 {
		maxBytes = 0
	}
	if len(s) <= maxBytes && utf8.ValidString(s) {
		return s
	}
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}
