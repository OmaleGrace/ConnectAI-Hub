package web

import (
	"strings"
	"testing"

	"connectai-hub/internal/ai"
)

func TestTrimHistoryKeepsNewestWithinBudget(t *testing.T) {
	var msgs []ai.Message
	for i := 0; i < 12; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		msgs = append(msgs, ai.Message{Role: role, Text: strings.Repeat("x", 2900)})
	}
	got := trimHistory(msgs)
	total := 0
	for _, m := range got {
		total += len(m.Text)
	}
	if total > historyCharBudget+historyItemMax {
		t.Fatalf("history not trimmed: %d chars", total)
	}
	if len(got) == 0 || got[0].Role != "user" {
		t.Fatal("history must start with a user turn")
	}
	if got[len(got)-1].Text != msgs[len(msgs)-1].Text {
		t.Fatal("newest message must be kept")
	}
}

func TestTrimHistoryShortensHugeMessage(t *testing.T) {
	got := trimHistory([]ai.Message{{Role: "user", Text: "hi"}, {Role: "model", Text: strings.Repeat("y", 20000)}})
	if len(got[1].Text) > historyItemMax+40 {
		t.Fatalf("long reply was not shortened: %d", len(got[1].Text))
	}
}

func TestLimitTextKeepsUTF8(t *testing.T) {
	s := strings.Repeat("é", 100)
	out, cut := limitText(s, 51)
	if !cut || len(out) > 51 || strings.ContainsRune(out, '\uFFFD') {
		t.Fatalf("bad cut: %q", out)
	}
}
