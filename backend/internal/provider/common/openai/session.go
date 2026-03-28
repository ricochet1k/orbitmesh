package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

// openDumpWriter opens a buffered writer for JSONL stream dumps.
// The path may contain a %s verb which is replaced with a timestamp so that
// each session turn gets its own file.  Returns nil, nil when dumping is
// disabled (i.e. OPENAI_STREAM_DUMP is not set).
func openDumpWriter(sessionID string, turn int64) (*bufio.Writer, func(), error) {
	path := os.Getenv("OPENAI_STREAM_DUMP")
	if path == "" {
		return nil, nil, nil
	}
	// Allow a %s placeholder that is replaced with "<sessionID>-t<turn>-<ts>".
	stamp := fmt.Sprintf("%s-t%d-%s", sessionID, turn, time.Now().UTC().Format("20060102T150405Z"))
	expanded := fmt.Sprintf(path, stamp)
	f, err := os.OpenFile(expanded, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("openai stream dump: %w", err)
	}
	w := bufio.NewWriterSize(f, 64*1024)
	flush := func() {
		_ = w.Flush()
		_ = f.Close()
	}
	return w, flush, nil
}

// Session is a single OpenAI chat conversation.  It maintains message history
// so that follow-up SendInput calls append to the same thread.
type Session struct {
	sessionID    string
	client       oai.Client
	model        shared.ChatModel
	systemPrompt string

	// history holds the full conversation as OpenAI message params.
	// Guarded by historyMu; only written by handleStream after a turn completes,
	// and read by SendInput at the start of the next turn.
	historyMu sync.Mutex
	history   []oai.ChatCompletionMessageParamUnion

	// turnCounter is incremented atomically on each SendInput call so that
	// every turn gets a unique message ID even across concurrent callers.
	turnCounter atomic.Int64

	liveStream *session.Stream

	mu     sync.Mutex
	state  *native.ProviderState
	events *native.EventAdapter
}

var _ session.Session = (*Session)(nil)

// newSession is the internal constructor used by Provider.CreateSession.
func newSession(
	sessionID string,
	client oai.Client,
	model string,
	systemPrompt string,
	resumeMessages []session.Message,
) *Session {
	s := &Session{
		sessionID:    sessionID,
		client:       client,
		model:        shared.ChatModel(model),
		systemPrompt: systemPrompt,
		state:        native.NewProviderState(),
		events:       native.NewEventAdapter(sessionID, 100),
		liveStream:   session.NewStream(),
	}

	// Seed history from resumed messages so the conversation can continue.
	for _, m := range resumeMessages {
		switch m.Kind {
		case session.MKSystem:
			s.history = append(s.history, oai.SystemMessage(m.Contents))
		case session.MKAssistant:
			s.history = append(s.history, oai.AssistantMessage(m.Contents))
		case session.MKUser:
			s.history = append(s.history, oai.UserMessage(m.Contents))
		case session.MKToolCall:
			// Contents holds the tool call as JSON: {"id":"...","name":"...","arguments":"..."}
			var tc struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(m.Contents), &tc); err == nil {
				p := oai.ChatCompletionAssistantMessageParam{}
				p.ToolCalls = []oai.ChatCompletionMessageToolCallUnionParam{
					{OfFunction: &oai.ChatCompletionMessageFunctionToolCallParam{
						ID: tc.ID,
						Function: oai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}},
				}
				s.history = append(s.history, oai.ChatCompletionMessageParamUnion{OfAssistant: &p})
			}
		case session.MKToolResponse:
			// Contents holds the tool result as JSON: {"tool_call_id":"...","content":"..."}
			var tr struct {
				ToolCallID string `json:"tool_call_id"`
				Content    string `json:"content"`
			}
			if err := json.Unmarshal([]byte(m.Contents), &tr); err == nil {
				s.history = append(s.history, oai.ToolMessage(tr.Content, tr.ToolCallID))
			}
		}
	}

	return s
}

// SendInput delivers user input to OpenAI and streams the response back as
// domain events.  On the first call it also injects the system prompt if one
// is configured.  Subsequent calls append to the existing conversation history.
//
// Fix: mutex is released before the HTTP call so it is not held during
// potentially-slow connection setup.
func (s *Session) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	// Snapshot history and compose the message list outside the lock.
	s.historyMu.Lock()
	var messages []oai.ChatCompletionMessageParamUnion
	if len(s.history) == 0 && s.systemPrompt != "" {
		messages = append(messages, oai.SystemMessage(s.systemPrompt))
	}
	messages = append(messages, s.history...)
	s.historyMu.Unlock()

	messages = append(messages, oai.UserMessage(input))

	s.state.SetState(session.StateRunning)
	s.events.Emit(domain.NewStatusChangeEvent(
		s.sessionID,
		domain.SessionStateIdle, domain.SessionStateRunning, "input received",
		nil,
	))

	// Build the filtered tool list from the global registry and session config.
	allDefs := tools.Global().List()
	filtered := tools.Filter(allDefs, config.AllowedTools, config.DisallowedTools)
	rendered := tools.RenderOpenAI(filtered)

	params := oai.ChatCompletionNewParams{
		Model:    s.model,
		Messages: messages,
		// Fix: request usage so the final chunk carries token counts.
		StreamOptions: oai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		},
	}
	if len(rendered) > 0 {
		params.Tools = rendered
	}

	// Fix: HTTP call is outside any lock.
	stream := s.client.Chat.Completions.NewStreaming(ctx, params)

	turn := s.turnCounter.Add(1)
	go s.handleStream(stream, messages, turn)

	return s.events.Events(), nil
}

// handleStream consumes the SSE stream and forwards domain events.
// sentMessages is the full message slice sent to OpenAI; it is extended with
// the completed assistant message to form the updated history.
// turn is the monotonically-increasing turn number, used to generate unique IDs.
func (s *Session) handleStream(
	stream *ssestream.Stream[oai.ChatCompletionChunk],
	sentMessages []oai.ChatCompletionMessageParamUnion,
	turn int64,
) {
	msgID := fmt.Sprintf("%s-t%d", s.sessionID, turn)
	var acc oai.ChatCompletionAccumulator

	// Open dump writer if OPENAI_STREAM_DUMP is set.  Errors are non-fatal;
	// we log them as a warning event and continue without dumping.
	dumpW, dumpClose, dumpErr := openDumpWriter(s.sessionID, turn)
	if dumpErr != nil {
		s.events.Emit(domain.NewErrorEvent(s.sessionID, dumpErr.Error(), "OPENAI_DUMP_OPEN_ERROR", nil))
	}
	if dumpClose != nil {
		defer dumpClose()
	}

	// Announce a new (empty) assistant message so the live stream has an entry
	// to append deltas into.
	s.liveStream.MessageNew(session.Message{
		ID:   msgID,
		Kind: session.MKAssistant,
	})

	for stream.Next() {
		chunk := stream.Current()

		// Dump raw chunk as JSONL when enabled.
		if dumpW != nil {
			if raw, err := json.Marshal(chunk); err == nil {
				_, _ = dumpW.Write(raw)
				_ = dumpW.WriteByte('\n')
			}
		}

		// Fix: check AddChunk return value; a false return means the chunk
		// could not be accumulated (e.g. out-of-order, mismatched ID).
		if !acc.AddChunk(chunk) {
			s.events.Emit(domain.NewErrorEvent(
				s.sessionID,
				"received out-of-order or mismatched stream chunk; accumulation may be incomplete",
				"OPENAI_ACCUMULATOR_ERROR",
				nil,
			))
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (sent when stream_options.include_usage=true).
			continue
		}

		delta := chunk.Choices[0].Delta

		// Fix: stream content deltas.
		if delta.Content != "" {
			s.liveStream.MessageAppend(session.Message{
				ID:       msgID,
				Kind:     session.MKAssistant,
				Contents: delta.Content,
			})
			s.events.Emit(domain.NewDeltaOutputEventForMessage(s.sessionID, msgID, delta.Content, nil))
		}

		// Fix: stream refusal deltas.
		if delta.Refusal != "" {
			s.liveStream.MessageAppend(session.Message{
				ID:       msgID,
				Kind:     session.MKAssistant,
				Contents: delta.Refusal,
			})
			s.events.Emit(domain.NewDeltaOutputEventForMessage(s.sessionID, msgID, delta.Refusal, nil))
		}

		// Fix: detect when a tool call has been fully assembled by the
		// accumulator and emit a ToolCall event.
		if tc, ok := acc.JustFinishedToolCall(); ok {
			inputJSON, _ := json.Marshal(map[string]string{
				"id":        tc.ID,
				"name":      tc.Name,
				"arguments": tc.Arguments,
			})
			s.liveStream.MessageNew(session.Message{
				ID:       fmt.Sprintf("%s-tc%d", msgID, tc.Index),
				Kind:     session.MKToolCall,
				Contents: string(inputJSON),
			})
			s.events.Emit(domain.NewToolCallEvent(s.sessionID, domain.ToolCallData{
				ID:     tc.ID,
				Name:   tc.Name,
				Status: "running",
				Input:  tc.Arguments,
			}, nil))
		}
	}

	if err := stream.Err(); err != nil {
		errMsg := fmt.Sprintf("stream error: %v", err)
		s.liveStream.MessageNew(session.Message{
			ID:       msgID + "-err",
			Kind:     session.MKError,
			Contents: errMsg,
		})
		s.events.Emit(domain.NewErrorEvent(s.sessionID, errMsg, "OPENAI_STREAM_ERROR", nil))
		s.state.SetError(err)
		s.events.Emit(domain.NewStatusChangeEvent(
			s.sessionID,
			domain.SessionStateRunning, domain.SessionStateIdle, "stream error",
			nil,
		))
		s.events.Close()
		s.liveStream.Close()
		return
	}

	// ── Post-stream processing ──────────────────────────────────────────────

	// Fix: emit token-usage metrics (only non-zero when include_usage=true).
	if usage := acc.Usage; usage.TotalTokens > 0 {
		s.events.Emit(domain.NewMetricEvent(
			s.sessionID,
			usage.PromptTokens,
			usage.CompletionTokens,
			1,
			nil,
		))
		s.events.Emit(domain.NewResourceUsageEvent(s.sessionID, domain.ResourceUsageData{
			Scope: "turn",
			Data: map[string]any{
				"source": "usage",
				"usage": map[string]any{
					"input_tokens":  usage.PromptTokens,
					"output_tokens": usage.CompletionTokens,
					"total_tokens":  usage.TotalTokens,
				},
				"model": s.model,
			},
		}, nil))
		s.state.AddTokens(usage.PromptTokens, usage.CompletionTokens)
	}

	// Fix: check FinishReason and surface non-normal stops as metadata events.
	if len(acc.Choices) > 0 {
		switch reason := acc.Choices[0].FinishReason; reason {
		case "length":
			s.events.Emit(domain.NewMetadataEvent(
				s.sessionID, "finish_reason", "length",
				nil,
			))
		case "content_filter":
			s.events.Emit(domain.NewMetadataEvent(
				s.sessionID, "finish_reason", "content_filter",
				nil,
			))
		case "refusal":
			s.events.Emit(domain.NewMetadataEvent(
				s.sessionID, "finish_reason", "refusal",
				nil,
			))
		}
	}

	// Fix: replace the live stream message with the fully assembled text (or
	// refusal) from the accumulator rather than the incrementally built string.
	if len(acc.Choices) > 0 {
		finalContent := acc.Choices[0].Message.Content
		if finalContent == "" {
			finalContent = acc.Choices[0].Message.Refusal
		}
		if finalContent != "" {
			s.liveStream.MessageReplace(session.Message{
				ID:       msgID,
				Kind:     session.MKAssistant,
				Contents: finalContent,
			})
		}
	}

	// Fix: use ToParam() to preserve ToolCalls in the assistant history entry,
	// which is required for OpenAI to accept subsequent multi-turn requests.
	if len(acc.Choices) > 0 {
		assistantParam := acc.Choices[0].Message.ToParam()
		s.historyMu.Lock()
		s.history = append(sentMessages, assistantParam)
		s.historyMu.Unlock()
	}

	s.state.SetState(session.StateStopped)
	s.events.Emit(domain.NewStatusChangeEvent(
		s.sessionID,
		domain.SessionStateRunning, domain.SessionStateIdle, "done",
		nil,
	))
	s.events.Close()
	s.liveStream.Close()
}

// Suspend captures the current provider state for persistence.
// For OpenAI sessions, there is no long-lived process state — the conversation
// history is already maintained in s.history.  We return a minimal context.
func (s *Session) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	return &session.SuspensionContext{
		Reason: "waiting for tool result",
	}, nil
}

// Resume restores a provider from a suspended state.
// For OpenAI sessions this is a no-op since history is already in memory.
func (s *Session) Resume(ctx context.Context, sc *session.SuspensionContext) error {
	return nil
}

// ResumeWithToolResults appends tool result messages to the conversation
// history, resets the event channel, re-calls the model, and returns the fresh
// event channel for the caller to consume (analogous to SendInput).
func (s *Session) ResumeWithToolResults(ctx context.Context, sc *session.SuspensionContext, results []session.ToolResult) (<-chan domain.Event, error) {
	// Append tool result messages to history under the history lock.
	s.historyMu.Lock()
	for _, r := range results {
		s.history = append(s.history, oai.ToolMessage(r.Result, r.ToolCallID))
	}
	var messages []oai.ChatCompletionMessageParamUnion
	messages = append(messages, s.history...)
	s.historyMu.Unlock()

	// Reset the event adapter so the new stream has a live channel to emit on.
	s.mu.Lock()
	s.events = native.NewEventAdapter(s.sessionID, 100)
	s.mu.Unlock()

	// Build the filtered tool list from the global registry.
	allDefs := tools.Global().List()
	rendered := tools.RenderOpenAI(allDefs)

	params := oai.ChatCompletionNewParams{
		Model:    s.model,
		Messages: messages,
		StreamOptions: oai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		},
	}
	if len(rendered) > 0 {
		params.Tools = rendered
	}

	stream := s.client.Chat.Completions.NewStreaming(ctx, params)

	turn := s.turnCounter.Add(1)
	go s.handleStream(stream, messages, turn)

	s.mu.Lock()
	ch := s.events.Events()
	s.mu.Unlock()
	return ch, nil
}

// Stop requests a graceful shutdown.  For OpenAI sessions this is equivalent
// to Kill since there is no long-lived server-side process to signal.
func (s *Session) Stop(ctx context.Context) error {
	return s.Kill()
}

// Kill immediately closes the event channel and stream.
func (s *Session) Kill() error {
	s.state.SetState(session.StateStopped)
	s.events.Close()
	s.liveStream.Close()
	return nil
}

// Status returns the current session status in a thread-safe manner.
func (s *Session) Status() session.Status {
	return s.state.Status()
}
