package openai

import (
	"context"
	"fmt"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type Session struct {
	provider     *Provider
	sessionID    string
	model        string
	conversation []*session.Message

	liveStream *session.Stream

	mu     sync.Mutex
	events *native.EventAdapter
}

var _ session.Session = (*Session)(nil)

// SendInput implements session.Session.  It fires an OpenAI streaming request
// and forwards events onto the returned channel.
func (s *Session) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	s.mu.Lock()
	if s.events == nil {
		// Use session ID from config on first call.
		sessID := config.TaskID
		if sessID == "" {
			sessID = "openai"
		}
		s.sessionID = sessID
		s.events = native.NewEventAdapter(sessID, 100)
	}
	s.mu.Unlock()

	stream := s.provider.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Background:   param.NewOpt(true),
		Instructions: param.NewOpt(""),
		ContextManagement: []responses.ResponseNewParamsContextManagement{
			{Type: "compaction", CompactThreshold: param.NewOpt[int64](2000000)},
		},
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				{OfInputMessage: &responses.ResponseInputItemMessageParam{
					Role: "system",
					Content: responses.ResponseInputMessageContentListParam{
						{OfInputText: &responses.ResponseInputTextParam{Text: ""}},
					},
				}},
				{OfMessage: &responses.EasyInputMessageParam{
					Role:    "user",
					Content: responses.EasyInputMessageContentUnionParam{OfString: param.NewOpt(input)},
				}},
			},
		},
		Model: openai.ChatModelGPT5_2,
	})

	s.liveStream = session.NewStream()
	go s.handleStream(stream)
	return s.events.Events(), nil
}

func (s *Session) handleStream(stream *ssestream.Stream[responses.ResponseStreamEventUnion]) {
	defer s.events.Close()

	messageId := "TODO"
	currentMessage := session.Message{
		ID:       messageId,
		Kind:     session.MKAssistant,
		Contents: "",
	}
	s.conversation = append(s.conversation, &currentMessage)

	for stream.Next() {
		data := stream.Current()
		switch data.AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			currentMessage.Contents += data.Delta
			s.liveStream.MessageAppend(session.Message{
				ID:       messageId,
				Contents: data.Delta,
			})
			s.events.Emit(domain.NewOutputEvent(s.sessionID, data.Delta, nil))
		case responses.ResponseTextDoneEvent:
			currentMessage.Contents = data.Text
			s.liveStream.MessageReplace(session.Message{
				ID:       messageId,
				Contents: data.Text,
			})
		default:
			msg := fmt.Sprintf("Unhandled stream event: %#v", data)
			s.liveStream.MessageNew(session.Message{
				ID:       messageId,
				Kind:     session.MKError,
				Contents: msg,
			})
		}
	}

	if err := stream.Err(); err != nil {
		errMsg := session.Message{
			ID:       messageId,
			Kind:     session.MKError,
			Contents: fmt.Sprintf("stream error: %v", err),
		}
		s.conversation = append(s.conversation, &errMsg)
		s.liveStream.MessageNew(errMsg)
		s.events.Emit(domain.NewErrorEvent(s.sessionID, err.Error(), "OPENAI_STREAM_ERROR", nil))
	}
}

func (s *Session) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events != nil {
		s.events.Close()
	}
	return nil
}

func (s *Session) Status() session.Status {
	return session.Status{State: session.StateRunning}
}

func (s *Session) Stop(ctx context.Context) error {
	return s.Kill()
}
