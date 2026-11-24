package web

import (
	"strings"
	"sync"
	"unicode/utf8"

	"connectai-hub/internal/ai"
)

// Limits that keep every AI request fast and cheap. They are generous enough
// that normal conversations are never affected.
const (
	historyMessages   = 12     // most recent messages considered as context
	historyCharBudget = 12000  // total characters of context sent per request (~3k tokens)
	historyItemMax    = 3000   // a single old message is shortened beyond this
	maxDocumentChars  = 120000 // extracted file text sent to the model (~30k tokens)
)

// limitText shortens s to at most max bytes without splitting a UTF-8 character.
func limitText(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// trimHistory keeps the newest messages that fit the character budget. Older
// answers are the most expensive part of a prompt and the least useful, so a
// long reply is shortened rather than resent in full on every turn.
func trimHistory(msgs []ai.Message) []ai.Message {
	kept := make([]ai.Message, 0, len(msgs))
	used := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		text, cut := limitText(strings.TrimSpace(msgs[i].Text), historyItemMax)
		if cut {
			text += "\n…[earlier text shortened]"
		}
		if text == "" {
			continue
		}
		if used+len(text) > historyCharBudget && len(kept) > 0 {
			break
		}
		used += len(text)
		kept = append(kept, ai.Message{Role: msgs[i].Role, Text: text})
	}
	// Restore chronological order.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	// A conversation should open with the user's turn.
	for len(kept) > 0 && kept[0].Role != "user" {
		kept = kept[1:]
	}
	return kept
}

// loadHistory returns recent context for a workspace in chronological order.
// beforeID > 0 restricts it to messages older than that id (used by regenerate).
func (s *Server) loadHistory(userID int64, slug string, beforeID int64) []ai.Message {
	query := `SELECT role, message FROM ai_messages WHERE user_id=$1 AND module_slug=$2 ORDER BY id DESC LIMIT $3`
	args := []any{userID, slug, historyMessages}
	if beforeID > 0 {
		query = `SELECT role, message FROM ai_messages WHERE user_id=$1 AND module_slug=$2 AND id<$4 ORDER BY id DESC LIMIT $3`
		args = append(args, beforeID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	newestFirst := make([]ai.Message, 0, historyMessages)
	for rows.Next() {
		var m ai.Message
		if rows.Scan(&m.Role, &m.Text) == nil {
			newestFirst = append(newestFirst, m)
		}
	}
	// Reverse into chronological order; trimHistory expects oldest-first input.
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return trimHistory(newestFirst)
}

// prepareChat loads the user's workspace settings and the conversation context
// at the same time instead of one after the other.
func (s *Server) prepareChat(userID int64, module Module, beforeID int64) (string, float64, []ai.Message) {
	var (
		wg          sync.WaitGroup
		instruction string
		temperature float64
		history     []ai.Message
	)
	wg.Add(2)
	go func() { defer wg.Done(); instruction, temperature = s.workspaceSettings(userID, module) }()
	go func() { defer wg.Done(); history = s.loadHistory(userID, module.Slug, beforeID) }()
	wg.Wait()
	return instruction, temperature, history
}

// saveExchange stores the user message, the answer and the usage record in one
// atomic statement (one database round trip) and returns the answer's id.
func (s *Server) saveExchange(userID int64, slug, userText, answer, requestType string, inputChars int) (int64, error) {
	var id int64
	err := s.db.QueryRow(`
		WITH saved AS (
			INSERT INTO ai_messages(user_id,module_slug,role,message)
			VALUES ($1,$2,'user',$3),($1,$2,'model',$4)
			RETURNING id, role
		), usage AS (
			INSERT INTO ai_usage(user_id,module_slug,request_type,input_characters,output_characters)
			VALUES ($1,$2,$5,$6,$7)
		)
		SELECT id FROM saved WHERE role='model'`,
		userID, slug, userText, answer, requestType, inputChars, len(answer)).Scan(&id)
	return id, err
}

// Module instructions never change at runtime, so build each one only once.
var instructionCache sync.Map

func cachedInstruction(m Module) string {
	if v, ok := instructionCache.Load(m.Slug); ok {
		return v.(string)
	}
	text := moduleInstruction(m)
	instructionCache.Store(m.Slug, text)
	return text
}
