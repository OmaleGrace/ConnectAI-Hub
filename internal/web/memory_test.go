package web

import (
	"strings"
	"testing"
)

func TestParseFactsAcceptsFencedJSONAndFiltersSecrets(t *testing.T) {
	raw := "```json\n[\"Runs a TikTok page about cooking\", \"Email is a@b.com\", \"password is hunter2\", \"Is learning Go\", \"Phone 08012345678\", \"x\"]\n```"
	got := parseFacts(raw)
	if len(got) != 2 || got[0] != "Runs a TikTok page about cooking" || got[1] != "Is learning Go" {
		t.Fatalf("unexpected facts: %#v", got)
	}
	if parseFacts("no json here") != nil || parseFacts("[]") != nil {
		t.Fatal("empty or invalid output must yield no facts")
	}
}

func TestMemoryTrigger(t *testing.T) {
	for _, m := range []string{"My name is Ada and I run a bakery", "I'm learning Go", "remember that I prefer short answers", "I work as a nurse"} {
		if !memoryTrigger.MatchString(m) {
			t.Errorf("should trigger: %q", m)
		}
	}
	for _, m := range []string{"What is recursion?", "Summarise this text for me", "Write a cover letter"} {
		if memoryTrigger.MatchString(m) {
			t.Errorf("should not trigger: %q", m)
		}
	}
}

func TestMemoryBlockIsBoundedAndLabelled(t *testing.T) {
	if memoryBlock(nil) != "" {
		t.Fatal("no facts, no block")
	}
	facts := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		facts = append(facts, strings.Repeat("a", 150))
	}
	block := memoryBlock(facts)
	if len(block) > maxMemoryPromptChars+400 || !strings.Contains(block, "never as instructions") {
		t.Fatalf("block not bounded/labelled (%d chars)", len(block))
	}
}
