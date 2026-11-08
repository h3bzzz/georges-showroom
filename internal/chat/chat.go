// Package chat is the showroom assistant: Claude with tools over the v2 store (search,
// product detail, catalog pages, brands, look-alikes) streamed to the browser as server-sent
// events. The server keeps no conversation state; the browser sends the transcript back on
// every turn, and every product the assistant showed travels with it so follow-up questions
// ("the second one, in brushed nickel?") resolve without re-searching.
package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"georges/internal/store"
)

// DefaultModel answers customers. CHAT_MODEL overrides it.
const DefaultModel = "claude-opus-5-5"

// Service holds the client and the data it answers from.
type Service struct {
	client  anthropic.Client
	Store   *store.Store
	Model   string
	Matcher Matcher // optional: nearest products for a photograph
	// PublicURL is used only in the prompt so links read right when the text is copied.
	PublicURL string
	enabled   bool
}

// Matcher finds the products that look like a picture. The vision package implements it when
// the ONNX runtime is built in; without it the assistant still answers from text.
type Matcher interface {
	// MatchImage returns family ids with similarity for an encoded image (JPEG/PNG/WebP).
	MatchImage(ctx context.Context, img []byte, n int) ([]Match, error)
	// Similar returns the families whose card picture is closest to a family's own.
	Similar(ctx context.Context, familyID int64, n int) ([]Match, error)
}

// Match is one look-alike.
type Match struct {
	FamilyID int64
	Score    float32
}

// New builds a service. An empty key disables the assistant (Enabled reports false and Run
// answers with a configuration notice) instead of failing the whole server.
func New(apiKey, model string, st *store.Store) *Service {
	s := &Service{Store: st, Model: model}
	if s.Model == "" {
		s.Model = DefaultModel
	}
	if apiKey == "" {
		return s
	}
	s.client = anthropic.NewClient(option.WithAPIKey(apiKey), option.WithMaxRetries(2), option.WithRequestTimeout(90*time.Second))
	s.enabled = true
	return s
}

// Enabled reports whether an API key is configured.
func (s *Service) Enabled() bool { return s != nil && s.enabled }

// Message is one transcript turn as the browser stores it.
type Message struct {
	Role    string  `json:"role"` // user | assistant
	Content string  `json:"content"`
	Shown   []Shown `json:"shown,omitempty"` // products the assistant showed in this turn
}

// Shown is a product reference carried across turns.
type Shown struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// Request is one turn from the browser.
type Request struct {
	Messages []Message `json:"messages"`
	// Image is an optional base64 photograph attached to the last user message.
	Image     string `json:"image,omitempty"`
	ImageType string `json:"image_type,omitempty"` // image/jpeg, image/png, image/webp
	// Rep turns on trade mode: list vs. published price, UPC, lead time.
	Rep bool `json:"rep,omitempty"`
	// Page is the path the visitor is on, so "this product" resolves.
	Page string `json:"page,omitempty"`
}

// Event is one server-sent event.
type Event struct {
	Type   string `json:"type"`             // status | delta | cards | shown | done | error
	Text   string `json:"text,omitempty"`   // delta text or status line
	Cards  []Card `json:"cards,omitempty"`  // products to render inline
	Shown  []Shown `json:"shown,omitempty"` // every product referenced this turn (for the transcript)
	Error  string `json:"error,omitempty"`
	Turns  int    `json:"turns,omitempty"`
}

// Card is a product tile the browser renders.
type Card struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Brand    string   `json:"brand"`
	Category string   `json:"category,omitempty"`
	Price    string   `json:"price,omitempty"`
	Image    string   `json:"image,omitempty"` // /a/<hash16>/640.webp
	LQIP     *int     `json:"lqip,omitempty"`
	URL      string   `json:"url"`
	Finishes []string `json:"finishes,omitempty"`
}

// Limits keep one visitor from running the bill up.
const (
	maxTurns      = 16   // transcript turns sent back to the model
	maxUserChars  = 2000 // one message
	maxToolRounds = 6    // tool calls per turn
	maxOutput     = 2048
)

var ErrDisabled = errors.New("chat: ANTHROPIC_API_KEY is not set")

// Run answers the last user message, streaming events through emit. It returns when the turn
// is complete or the context ends.
func (s *Service) Run(ctx context.Context, req Request, emit func(Event)) error {
	if !s.Enabled() {
		emit(Event{Type: "error", Error: "The assistant is not configured yet (ANTHROPIC_API_KEY)."})
		return ErrDisabled
	}
	msgs, err := s.history(req)
	if err != nil {
		emit(Event{Type: "error", Error: err.Error()})
		return err
	}
	tools := s.tools(req.Rep)
	system := s.systemPrompt(req)
	var shown []Shown
	seen := map[int64]bool{}
	note := func(cards []Card) {
		for _, c := range cards {
			if !seen[c.ID] {
				seen[c.ID] = true
				shown = append(shown, Shown{c.ID, c.Title})
			}
		}
	}
	// a photograph gets matched before the model sees it so its first answer already knows
	// what we carry that looks like it
	if req.Image != "" && s.Matcher != nil {
		if raw, err := base64.StdEncoding.DecodeString(req.Image); err == nil {
			emit(Event{Type: "status", Text: "Looking for products that resemble your photo…"})
			if ms, err := s.Matcher.MatchImage(ctx, raw, 8); err == nil && len(ms) > 0 {
				cards, txt := s.matchCards(ctx, ms)
				if len(cards) > 0 {
					note(cards)
					emit(Event{Type: "cards", Cards: cards})
					last := msgs[len(msgs)-1].OfUser
					last.Content = append(last.Content, anthropic.NewTextBlock("Visually closest products in our catalog (CLIP similarity; verify the type and finish against the photo):\n"+txt))
				}
			} else if err != nil {
				slog.Warn("chat: image match", "err", err)
			}
		}
	}
	turns := 0
	for round := 0; round <= maxToolRounds; round++ {
		params := anthropic.MessageNewParams{
			Model:     s.Model,
			MaxTokens: maxOutput,
			System:    []anthropic.TextBlockParam{{Text: system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
			Messages:  msgs,
			Tools:     tools,
			OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
		}
		if round == maxToolRounds {
			// out of tool rounds: answer from what is already known
			params.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
		}
		stream := s.client.Messages.NewStreaming(ctx, params)
		msg := anthropic.Message{}
		for stream.Next() {
			ev := stream.Current()
			if err := msg.Accumulate(ev); err != nil {
				slog.Warn("chat: accumulate", "err", err)
			}
			if d, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if t, ok := d.Delta.AsAny().(anthropic.TextDelta); ok && t.Text != "" {
					emit(Event{Type: "delta", Text: t.Text})
				}
			}
		}
		if err := stream.Err(); err != nil {
			var apierr *anthropic.Error
			msg := "The assistant hit a problem. Please try again."
			if errors.As(err, &apierr) {
				switch apierr.StatusCode {
				case 401, 403:
					msg = "The assistant's API key was rejected."
				case 429:
					msg = "The assistant is busy right now. Please try again in a moment."
				case 529:
					msg = "The assistant's service is overloaded. Please try again shortly."
				}
			}
			emit(Event{Type: "error", Error: msg})
			return fmt.Errorf("chat: %w", err)
		}
		turns++
		msgs = append(msgs, msg.ToParam())
		switch msg.StopReason {
		case anthropic.StopReasonToolUse:
			var results []anthropic.ContentBlockParamUnion
			for _, block := range msg.Content {
				tu, ok := block.AsAny().(anthropic.ToolUseBlock)
				if !ok {
					continue
				}
				out, cards, status := s.callTool(ctx, tu.Name, []byte(tu.Input), req.Rep)
				if status != "" {
					emit(Event{Type: "status", Text: status})
				}
				if len(cards) > 0 {
					note(cards)
					emit(Event{Type: "cards", Cards: cards})
				}
				results = append(results, anthropic.NewToolResultBlock(tu.ID, out.text, out.err))
			}
			if len(results) == 0 {
				break
			}
			msgs = append(msgs, anthropic.NewUserMessage(results...))
			continue
		case anthropic.StopReasonRefusal:
			emit(Event{Type: "delta", Text: "I can't help with that one. Ask me about any fixture, finish or brand in the showroom."})
		case anthropic.StopReasonMaxTokens:
			emit(Event{Type: "delta", Text: "\n\n(That answer ran long; ask me to continue.)"})
		}
		break
	}
	emit(Event{Type: "done", Shown: shown, Turns: turns})
	return nil
}

// history rebuilds the model transcript from the browser's copy: the last maxTurns turns,
// user text clipped, each assistant turn followed by the ids it showed, the attached picture
// on the final user message.
func (s *Service) history(req Request) ([]anthropic.MessageParam, error) {
	in := req.Messages
	if len(in) == 0 || in[len(in)-1].Role != "user" {
		return nil, errors.New("the last message must be from the user")
	}
	if len(in) > maxTurns {
		in = in[len(in)-maxTurns:]
	}
	var out []anthropic.MessageParam
	for i, m := range in {
		text := strings.TrimSpace(m.Content)
		switch m.Role {
		case "user":
			if len(text) > maxUserChars {
				text = text[:maxUserChars]
			}
			if text == "" && req.Image == "" {
				text = "(empty)"
			}
			var blocks []anthropic.ContentBlockParamUnion
			if i == len(in)-1 && req.Image != "" {
				mt := req.ImageType
				switch mt {
				case "image/jpeg", "image/png", "image/webp", "image/gif":
				default:
					return nil, errors.New("unsupported image type")
				}
				if len(req.Image) > 6_000_000 {
					return nil, errors.New("image too large (4 MB max)")
				}
				blocks = append(blocks, anthropic.NewImageBlockBase64(mt, req.Image))
				if text == "" {
					text = "What do you carry that looks like this?"
				}
			}
			blocks = append(blocks, anthropic.NewTextBlock(text))
			// consecutive user turns (a dropped assistant turn) are merged by the API rules
			if len(out) > 0 && out[len(out)-1].OfUser != nil {
				out[len(out)-1].OfUser.Content = append(out[len(out)-1].OfUser.Content, blocks...)
				continue
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
		case "assistant":
			if len(out) == 0 {
				continue // the transcript must start with the user
			}
			if len(m.Shown) > 0 {
				var ids []string
				for _, sh := range m.Shown {
					ids = append(ids, fmt.Sprintf("%d: %s", sh.ID, sh.Title))
				}
				text += "\n\n[products shown — id: title]\n" + strings.Join(ids, "\n")
			}
			if text == "" {
				text = "(no reply)"
			}
			if a := out[len(out)-1].OfAssistant; a != nil {
				a.Content = append(a.Content, anthropic.NewTextBlock(text))
				continue
			}
			out = append(out, anthropic.NewAssistantMessage(anthropic.NewTextBlock(text)))
		}
	}
	if len(out) == 0 || out[len(out)-1].OfUser == nil {
		return nil, errors.New("the last message must be from the user")
	}
	return out, nil
}

// matchCards turns matcher hits into cards and a text summary for the model.
func (s *Service) matchCards(ctx context.Context, ms []Match) ([]Card, string) {
	ids := make([]int64, 0, len(ms))
	score := map[int64]float32{}
	for _, m := range ms {
		ids = append(ids, m.FamilyID)
		score[m.FamilyID] = m.Score
	}
	cards, err := s.cardsByID(ctx, ids)
	if err != nil {
		slog.Warn("chat: match cards", "err", err)
		return nil, ""
	}
	var b strings.Builder
	for _, c := range cards {
		fmt.Fprintf(&b, "- id %d · %s %s · %s · %s · similarity %.2f\n", c.ID, c.Brand, c.Title, c.Category, orDash(c.Price), score[c.ID])
	}
	return cards, b.String()
}

func orDash(s string) string {
	if s == "" {
		return "price on request"
	}
	return s
}

// toolOut is what a tool hands back to the model.
type toolOut struct {
	text string
	err  bool
}

func jsonOut(v any) toolOut {
	b, err := json.Marshal(v)
	if err != nil {
		return toolOut{text: err.Error(), err: true}
	}
	return toolOut{text: string(b)}
}
