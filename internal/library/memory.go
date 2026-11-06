package library

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// User memory: what Phoenix has learned about the person across chats, kept as small separate
// entries so the user can read, correct and delete each one in Settings → Memory and the agent
// can use them without asking again. An entry is one of:
//
//	fact        something true about the user ("Phone: +92 …", "Authorized to work in the UK")
//	answer      their answer to a question a form or the agent asked (key = the question)
//	preference  how they like things done ("Prefers remote roles")
//	choice      what they tapped or typed when offered options (recorded automatically)
//
// A budgeted digest of the most useful entries goes into every run's instructions; the rest is
// reachable through memory_recall. Memory stays in the local library database.

// Entry kinds.
const (
	MemFact       = "fact"
	MemAnswer     = "answer"
	MemPreference = "preference"
	MemChoice     = "choice"
)

const (
	maxMemories  = 600
	maxMemTopic  = 60
	maxMemKey    = 200
	maxMemValue  = 500
	choiceTopic  = "Choices"
	defaultTopic = "General"
)

// MemoryEntry is one remembered item.
type MemoryEntry struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	Topic   string `json:"topic"`
	Key     string `json:"key"`
	Value   string `json:"value"`
	Uses    int    `json:"uses"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
}

var memMu sync.Mutex

func tidy(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

func validKind(k string) bool {
	switch k {
	case MemFact, MemAnswer, MemPreference, MemChoice:
		return true
	}
	return false
}

// Remember adds an entry, or updates the one with the same kind, topic and key. Saving the same
// value again counts as a use, so what the user keeps confirming rises to the top.
func (s *Store) Remember(kind, topic, key, value string) (MemoryEntry, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = MemFact
	}
	if !validKind(kind) {
		return MemoryEntry{}, fmt.Errorf("kind must be fact, answer, preference or choice")
	}
	topic = tidy(topic, maxMemTopic)
	if topic == "" {
		topic = defaultTopic
	}
	key = tidy(key, maxMemKey)
	value = tidy(value, maxMemValue)
	if value == "" {
		return MemoryEntry{}, fmt.Errorf("give the value to remember")
	}
	memMu.Lock()
	defer memMu.Unlock()
	var id int64
	var oldValue string
	var uses int
	err := s.db.QueryRow(`SELECT id, value, uses FROM memories WHERE kind=? AND topic=? COLLATE NOCASE AND key=? COLLATE NOCASE`, kind, topic, key).Scan(&id, &oldValue, &uses)
	switch {
	case err == nil:
		if strings.EqualFold(oldValue, value) {
			uses++
		} else {
			uses = 1 // a changed answer starts counting again
		}
		if _, err = s.db.Exec(`UPDATE memories SET value=?, uses=?, updated_at=? WHERE id=?`, value, uses, now(), id); err != nil {
			return MemoryEntry{}, err
		}
	case errors.Is(err, sql.ErrNoRows):
		res, err := s.db.Exec(`INSERT INTO memories(kind,topic,key,value,uses,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`, kind, topic, key, value, now(), now())
		if err != nil {
			return MemoryEntry{}, err
		}
		id, _ = res.LastInsertId()
		s.pruneMemories()
	default:
		return MemoryEntry{}, err
	}
	return s.memoryByID(id)
}

// RecordChoice notes what the user picked (or typed) when asked a question, so the agent can
// learn their habits and stop asking what they always answer the same way.
func (s *Store) RecordChoice(question, picked string) error {
	picked = tidy(picked, maxMemValue)
	if picked == "" {
		return nil
	}
	question = tidy(question, maxMemKey)
	if question == "" {
		question = "(options offered)"
	}
	_, err := s.Remember(MemChoice, choiceTopic, question, picked)
	return err
}

// pruneMemories keeps the table bounded: over the limit, the least used automatic choices go first.
func (s *Store) pruneMemories() {
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&n) != nil || n <= maxMemories {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM memories WHERE id IN (SELECT id FROM memories ORDER BY (kind='choice') DESC, uses ASC, updated_at ASC LIMIT ?)`, n-maxMemories)
}

func scanMemory(sc interface{ Scan(...any) error }) (MemoryEntry, error) {
	var m MemoryEntry
	err := sc.Scan(&m.ID, &m.Kind, &m.Topic, &m.Key, &m.Value, &m.Uses, &m.Created, &m.Updated)
	return m, err
}

const memCols = `id, kind, topic, key, value, uses, created_at, updated_at`

func (s *Store) memoryByID(id int64) (MemoryEntry, error) {
	return scanMemory(s.db.QueryRow(`SELECT `+memCols+` FROM memories WHERE id=?`, id))
}

// Memories returns every entry, by topic, then most recently changed first.
func (s *Store) Memories() ([]MemoryEntry, error) {
	rows, err := s.db.Query(`SELECT ` + memCols + ` FROM memories ORDER BY topic COLLATE NOCASE, updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MemoryEntry{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchMemories finds entries whose topic, key or value contains every word of query.
func (s *Store) SearchMemories(query string, limit int) ([]MemoryEntry, error) {
	all, err := s.Memories()
	if err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(query))
	out := []MemoryEntry{}
	for _, m := range all {
		hay := strings.ToLower(m.Topic + " " + m.Key + " " + m.Value)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// UpdateMemory edits one entry in place (the Settings page).
func (s *Store) UpdateMemory(id int64, topic, key, value string) error {
	topic, key, value = tidy(topic, maxMemTopic), tidy(key, maxMemKey), tidy(value, maxMemValue)
	if topic == "" {
		topic = defaultTopic
	}
	if value == "" {
		return fmt.Errorf("the value cannot be empty")
	}
	memMu.Lock()
	defer memMu.Unlock()
	res, err := s.db.Exec(`UPDATE memories SET topic=?, key=?, value=?, updated_at=? WHERE id=?`, topic, key, value, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("that memory no longer exists")
	}
	return nil
}

// ForgetMemory deletes one entry.
func (s *Store) ForgetMemory(id int64) error {
	memMu.Lock()
	defer memMu.Unlock()
	_, err := s.db.Exec(`DELETE FROM memories WHERE id=?`, id)
	return err
}

// ClearMemories deletes everything remembered.
func (s *Store) ClearMemories() error {
	memMu.Lock()
	defer memMu.Unlock()
	_, err := s.db.Exec(`DELETE FROM memories`)
	return err
}

func (m MemoryEntry) line() string {
	switch {
	case m.Kind == MemChoice:
		if m.Uses >= 2 {
			return fmt.Sprintf("When asked \"%s\" they usually choose \"%s\" (%d times)", m.Key, m.Value, m.Uses)
		}
		return fmt.Sprintf("When asked \"%s\" they chose \"%s\"", m.Key, m.Value)
	case m.Key != "":
		return m.Key + ": " + m.Value
	}
	return m.Value
}

// MemoryDigest is the block added to the agent's instructions: the entries most worth knowing,
// grouped by topic, within budget characters. "" when nothing is remembered.
func (s *Store) MemoryDigest(budget int) string {
	all, err := s.Memories()
	if err != nil || len(all) == 0 {
		return ""
	}
	// Most useful first: confirmed or reused entries, then recent ones; automatic choices last.
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if (a.Kind == MemChoice) != (b.Kind == MemChoice) {
			return b.Kind == MemChoice
		}
		if a.Uses != b.Uses {
			return a.Uses > b.Uses
		}
		return a.Updated > b.Updated
	})
	const head = "WHAT YOU REMEMBER ABOUT THE USER (from earlier chats). Use it to answer forms and decide without asking again; follow their habits and choices. If something here is outdated, save the correction with memory_remember; look up anything not listed with memory_recall.\n"
	used := len([]rune(head))
	byTopic := map[string][]string{}
	var order []string
	omitted := 0
	for _, m := range all {
		l := "- " + m.line() + "\n"
		cost := len([]rune(l)) + 4
		if _, seen := byTopic[m.Topic]; !seen {
			cost += len([]rune(m.Topic)) + 5
		}
		if used+cost > budget {
			omitted++
			continue
		}
		used += cost
		if _, seen := byTopic[m.Topic]; !seen {
			order = append(order, m.Topic)
		}
		byTopic[m.Topic] = append(byTopic[m.Topic], l)
	}
	var b strings.Builder
	b.WriteString(head)
	for _, t := range order {
		b.WriteString("## " + t + "\n")
		for _, l := range byTopic[t] {
			b.WriteString(l)
		}
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "(%d more not shown: use memory_recall)\n", omitted)
	}
	return b.String()
}

type memRememberArgs struct {
	Value string `json:"value" jsonschema:"what to remember, short: the user's answer or the fact itself"`
	Key   string `json:"key,omitempty" jsonschema:"for an answer, the question it answers (for example: Are you authorised to work in the US?); for a fact, a label such as Phone or LinkedIn"`
	Topic string `json:"topic,omitempty" jsonschema:"the section it belongs to, for example Contact, Job applications, Work authorisation, Salary, Preferences, Education"`
	Kind  string `json:"kind,omitempty" jsonschema:"fact (default), answer, or preference"`
}

type memRecallArgs struct {
	Query string `json:"query,omitempty" jsonschema:"words to look for in topic, question or value; empty lists everything remembered"`
}

type memForgetArgs struct {
	Query string `json:"query" jsonschema:"words that identify the entry to delete; every entry matching all the words is deleted"`
}

func registerMemoryTools(srv *sdk.Server, s *Store) {
	tool(srv, "memory_remember", "Save something lasting about the user so you never ask twice and can follow their habits: contact details, answers to application questions (work authorisation, salary, notice period, why they want a role), preferences, a real example they gave. Call it in the same turn you learn it. Saving the same key again corrects it.", func(a memRememberArgs) (string, error) {
		kind := a.Kind
		if kind == MemChoice {
			kind = MemAnswer // choices are recorded by Phoenix itself
		}
		m, err := s.Remember(kind, a.Topic, a.Key, a.Value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Remembered under %s.", m.Topic), nil
	})
	tool(srv, "memory_recall", "Look up what is remembered about the user. The most useful entries are already in your instructions; use this for anything not listed, or to list a whole topic.", func(a memRecallArgs) (string, error) {
		ms, err := s.SearchMemories(a.Query, 60)
		if err != nil {
			return "", err
		}
		if len(ms) == 0 {
			return "Nothing remembered for that.", nil
		}
		var b strings.Builder
		for _, m := range ms {
			fmt.Fprintf(&b, "[%s] %s\n", m.Topic, m.line())
		}
		return b.String(), nil
	})
	tool(srv, "memory_forget", "Delete remembered entries that are wrong or that the user asked you to forget.", func(a memForgetArgs) (string, error) {
		if strings.TrimSpace(a.Query) == "" {
			return "", fmt.Errorf("say which memory to forget")
		}
		ms, err := s.SearchMemories(a.Query, 20)
		if err != nil {
			return "", err
		}
		for _, m := range ms {
			if err := s.ForgetMemory(m.ID); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Forgot %d.", len(ms)), nil
	})
}
