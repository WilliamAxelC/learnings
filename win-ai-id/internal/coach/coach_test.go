package coach

import (
	"database/sql"
	"strings"
	"testing"

	"winaiid/internal/store"
)

func TestRuleBasedPrefersMostMissedHabit(t *testing.T) {
	hab := sql.NullInt64{Int64: 1, Valid: true}
	in := Input{
		Today: "2026-10-07",
		History: []store.Win{
			{Day: "2026-10-05", Title: "Gym", HabitID: hab},
			{Day: "2026-10-05", Title: "Read", HabitID: hab},
			{Day: "2026-10-06", Title: "Read", HabitID: hab},
			{Day: "2026-10-06", Title: "Walk", HabitID: hab, Done: true},
		},
	}
	out := RuleBased(in, "")
	if !out.Fallback || len(out.Proposals) != 2 || !strings.Contains(out.Proposals[0].Title, "Read") {
		t.Fatalf("got %+v", out)
	}
}

func TestParseResponseToolCallAndCost(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"role":"assistant","content":"Morning! Keep it light.",
	  "tool_calls":[{"id":"1","type":"function","function":{"name":"propose_wins",
	  "arguments":"{\"wins\":[{\"title\":\"Walk 10 min\",\"reason\":\"easy\"},{\"title\":\"\"},{\"title\":\"a\"},{\"title\":\"b\"},{\"title\":\"c\"}]}"}}]}}],
	  "usage":{"prompt_tokens":1000000,"completion_tokens":0}}`)
	out, err := parseResponse(raw, Config{InPricePerMTok: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Proposals) != MaxProposals || out.Proposals[0].Title != "Walk 10 min" {
		t.Fatalf("proposals: %+v", out.Proposals)
	}
	if out.CostMicros != 300000 { // $0.30
		t.Fatalf("cost: %d", out.CostMicros)
	}
}
