// Package coach turns the user's context into a short reply plus up to three
// proposed wins. It uses an OpenAI-compatible chat API when configured and
// within budget, and falls back to deterministic rule-based suggestions.
//
// The coach only ever returns proposals; persisting them as wins requires an
// explicit user accept (see store.DecideProposal).
package coach

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"winaiid/internal/store"
)

// MaxProposals caps suggestions per turn ("1–3 wins").
const MaxProposals = 3

// Proposal is one suggested win.
type Proposal struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// Input is everything the coach may see. Keep it small: it's the prompt.
type Input struct {
	Today       string
	Weekday     string
	About       string
	Habits      []store.Habit
	History     []store.Win // last 14 days, oldest first
	TodayWins   []store.Win
	Memory      []string
	Chat        []store.Message // today's prior turns
	UserMessage string          // empty for the automatic morning plan
}

// Output is the coach's answer.
type Output struct {
	Reply      string
	Proposals  []Proposal
	CostMicros int64
	Fallback   bool // true when rule-based suggestions were used
}

// Budget tracks monthly LLM spend.
type Budget interface {
	MonthCostMicros(ctx context.Context, month string) (int64, error)
	AddCostMicros(ctx context.Context, month string, micros int64) error
}

// Config configures the LLM backend. An empty BaseURL/APIKey disables it.
type Config struct {
	BaseURL         string  // e.g. https://generativelanguage.googleapis.com/v1beta/openai
	APIKey          string
	Model           string
	BudgetUSD       float64 // hard monthly cap
	InPricePerMTok  float64 // USD per 1M prompt tokens
	OutPricePerMTok float64 // USD per 1M completion tokens
}

// Coach plans wins.
type Coach struct {
	cfg    Config
	budget Budget
	http   *http.Client
}

// New builds a Coach.
func New(cfg Config, budget Budget) *Coach {
	return &Coach{cfg: cfg, budget: budget, http: &http.Client{Timeout: 30 * time.Second}}
}

// Enabled reports whether an LLM is configured.
func (c *Coach) Enabled() bool { return c.cfg.BaseURL != "" && c.cfg.APIKey != "" && c.cfg.Model != "" }

// Plan produces a reply and proposals. It never returns an error: failures
// degrade to rule-based output so the morning check-in always works.
func (c *Coach) Plan(ctx context.Context, in Input) Output {
	return c.PlanStream(ctx, in, nil)
}

// PlanStream produces a reply and proposals, streaming response chunks via onChunk if provided.
func (c *Coach) PlanStream(ctx context.Context, in Input, onChunk func(string)) Output {
	if !c.Enabled() {
		out := RuleBased(in, "")
		if onChunk != nil {
			onChunk(out.Reply)
		}
		return out
	}
	month := time.Now().UTC().Format("2006-01")
	spent, err := c.budget.MonthCostMicros(ctx, month)
	if err == nil && spent >= int64(c.cfg.BudgetUSD*1e6) {
		out := RuleBased(in, "Monthly AI budget reached, so these are simple suggestions until next month.")
		if onChunk != nil {
			onChunk(out.Reply)
		}
		return out
	}
	out, err := c.callLLMStream(ctx, in, onChunk)
	if out.CostMicros > 0 {
		_ = c.budget.AddCostMicros(ctx, month, out.CostMicros)
	}
	if err != nil {
		out = RuleBased(in, "The coach is unavailable right now, so here are simple suggestions.")
		if onChunk != nil {
			onChunk(out.Reply)
		}
		return out
	}
	return out
}

// ---- rule-based fallback -------------------------------------------------

// RuleBased suggests the habits missed most in recent history, shrunk to a
// 5-minute version, or a tiny generic win when nothing was missed.
func RuleBased(in Input, prefix string) Output {
	onToday := map[string]bool{}
	for _, w := range in.TodayWins {
		onToday[strings.ToLower(w.Title)] = true
	}
	misses := map[string]int{}
	var order []string
	for _, w := range in.History {
		if w.HabitID.Valid && !w.Done && w.Day != in.Today {
			if misses[w.Title] == 0 {
				order = append(order, w.Title)
			}
			misses[w.Title]++
		}
	}
	// Most-missed first (stable for ties).
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && misses[order[j]] > misses[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var props []Proposal
	for _, t := range order {
		title := "5-minute version: " + t
		if onToday[strings.ToLower(title)] {
			continue
		}
		props = append(props, Proposal{Title: title, Reason: fmt.Sprintf("Missed %d× in the last two weeks, so make it tiny.", misses[t])})
		if len(props) == MaxProposals {
			break
		}
	}
	if len(props) == 0 {
		props = []Proposal{{Title: "Clear one small surface (desk, sink, or inbox)", Reason: "A quick, visible win to start the day."}}
	}
	reply := "Here are a couple of small wins for today. Accept the ones that fit."
	if prefix != "" {
		reply = prefix + " " + reply
	}
	return Output{Reply: reply, Proposals: props, Fallback: true}
}

// ---- LLM -----------------------------------------------------------------

const systemPrompt = `You are the coach inside win.ai.id, an app for small daily wins.
Rules:
- Propose at most 3 wins for TODAY via the propose_wins tool. Each must be small (doable in under 30 minutes), concrete, and phrased as an action.
- Prefer shrinking habits the user keeps missing over adding new things. Don't duplicate items already on today's list.
- Reply in 1–3 short sentences, warm but not cheesy. No emoji spam, no lectures.
- Reply in the same language the user writes in (default English).
- If the user just chats without wanting a plan, you may call propose_wins with an empty list.`

type chatMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

var proposeTool = map[string]any{
	"type": "function",
	"function": map[string]any{
		"name":        "propose_wins",
		"description": "Propose up to 3 small wins for today. The user will accept or edit each one.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"wins": map[string]any{
					"type":     "array",
					"maxItems": MaxProposals,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"title":  map[string]any{"type": "string", "description": "Short action, e.g. 'Walk 10 minutes after lunch'"},
							"reason": map[string]any{"type": "string", "description": "One short line on why"},
						},
						"required": []string{"title"},
					},
				},
			},
			"required": []string{"wins"},
		},
	},
}

// BuildContext renders the user context block sent to the model.
func BuildContext(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Today: %s (%s)\n", in.Today, in.Weekday)
	if in.About != "" {
		fmt.Fprintf(&b, "\nAbout the user:\n%s\n", in.About)
	}
	if len(in.Memory) > 0 {
		b.WriteString("\nLong-term memory notes:\n")
		for _, m := range in.Memory {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}
	if len(in.Habits) > 0 {
		b.WriteString("\nHabits:\n")
		for _, h := range in.Habits {
			fmt.Fprintf(&b, "- %s (%s)\n", h.Title, h.Describe())
		}
	}
	if len(in.History) > 0 {
		b.WriteString("\nLast 14 days ([x]=done, [ ]=missed):\n")
		cur := ""
		for _, w := range in.History {
			if w.Day != cur {
				cur = w.Day
				fmt.Fprintf(&b, "%s:", cur)
			}
			mark := "[ ]"
			if w.Done {
				mark = "[x]"
			}
			fmt.Fprintf(&b, " %s %s;", mark, w.Title)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nAlready on today's list:\n")
	if len(in.TodayWins) == 0 {
		b.WriteString("(nothing yet)\n")
	}
	for _, w := range in.TodayWins {
		mark := "[ ]"
		if w.Done {
			mark = "[x]"
		}
		fmt.Fprintf(&b, "- %s %s\n", mark, w.Title)
	}
	return b.String()
}

func (c *Coach) callLLMStream(ctx context.Context, in Input, onChunk func(string)) (Output, error) {
	msgs := []chatMsg{
		{Role: "system", Content: systemPrompt},
		{Role: "system", Content: BuildContext(in)},
	}
	for _, m := range in.Chat {
		msgs = append(msgs, chatMsg{Role: m.Role, Content: m.Content})
	}
	user := in.UserMessage
	if user == "" {
		user = "Good morning. Plan my day's small wins."
	}
	msgs = append(msgs, chatMsg{Role: "user", Content: user})

	body, _ := json.Marshal(map[string]any{
		"model":       c.cfg.Model,
		"messages":    msgs,
		"tools":       []any{proposeTool},
		"tool_choice": "auto",
		"temperature": 0.6,
		"stream":      onChunk != nil,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Output{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return Output{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return Output{}, fmt.Errorf("llm http %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	if onChunk == nil {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return parseResponse(raw, c.cfg)
	}

	scanner := bufio.NewScanner(resp.Body)
	var replyBuilder strings.Builder
	var toolArgBuilder strings.Builder
	var inToolCall bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			replyBuilder.WriteString(delta.Content)
			onChunk(delta.Content)
		}
		for _, tc := range delta.ToolCalls {
			if tc.Function.Name == "propose_wins" || inToolCall {
				inToolCall = true
				toolArgBuilder.WriteString(tc.Function.Arguments)
			}
		}
	}

	out := Output{Reply: strings.TrimSpace(replyBuilder.String())}
	approxTokens := int64(len(replyBuilder.String())/4 + 400)
	out.CostMicros = int64(float64(approxTokens) * c.cfg.OutPricePerMTok)

	if toolArgBuilder.Len() > 0 {
		var args struct {
			Wins []Proposal `json:"wins"`
		}
		if err := json.Unmarshal([]byte(toolArgBuilder.String()), &args); err == nil {
			for _, p := range args.Wins {
				p.Title = truncate(strings.TrimSpace(p.Title), 120)
				p.Reason = truncate(strings.TrimSpace(p.Reason), 200)
				if p.Title != "" && len(out.Proposals) < MaxProposals {
					out.Proposals = append(out.Proposals, p)
				}
			}
		}
	}
	if out.Reply == "" && len(out.Proposals) > 0 {
		out.Reply = "Here's a small plan for today."
		onChunk(out.Reply)
	}
	if out.Reply == "" {
		return out, fmt.Errorf("llm stream: empty reply")
	}
	return out, nil
}

func parseResponse(raw []byte, cfg Config) (Output, error) {
	var r struct {
		Choices []struct {
			Message chatMsg `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Output{}, err
	}
	out := Output{CostMicros: int64(float64(r.Usage.PromptTokens)*cfg.InPricePerMTok + float64(r.Usage.CompletionTokens)*cfg.OutPricePerMTok)}
	if len(r.Choices) == 0 {
		return out, fmt.Errorf("llm: no choices")
	}
	m := r.Choices[0].Message
	out.Reply = strings.TrimSpace(m.Content)
	for _, tc := range m.ToolCalls {
		if tc.Function.Name != "propose_wins" {
			continue
		}
		var args struct {
			Wins []Proposal `json:"wins"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			continue
		}
		for _, p := range args.Wins {
			p.Title = truncate(strings.TrimSpace(p.Title), 120)
			p.Reason = truncate(strings.TrimSpace(p.Reason), 200)
			if p.Title != "" && len(out.Proposals) < MaxProposals {
				out.Proposals = append(out.Proposals, p)
			}
		}
	}
	if out.Reply == "" && len(out.Proposals) > 0 {
		out.Reply = "Here's a small plan for today."
	}
	if out.Reply == "" {
		return out, fmt.Errorf("llm: empty reply")
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
