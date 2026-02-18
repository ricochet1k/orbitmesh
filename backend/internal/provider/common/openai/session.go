package openai

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type Session struct {
	provider     *Provider
	model        string
	conversation []*session.Message

	liveStream *session.Stream
}

var _ session.Session = (*Session)(nil)

func (s *Session) Start(ctx context.Context, config session.Config) error {
	return nil
}

func (s *Session) SendInput(ctx context.Context, input string) error {
	stream := s.provider.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Background:   param.NewOpt(true),
		Instructions: param.NewOpt(""),
		ContextManagement: []responses.ResponseNewParamsContextManagement{
			{Type: "compaction", CompactThreshold: param.NewOpt[int64](2000000)},
		},
		// PreviousResponseID: "",
		// Conversation: responses.ResponseNewParamsConversationUnion{},
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				{OfInputMessage: &responses.ResponseInputItemMessageParam{
					Role: "system",
					Content: responses.ResponseInputMessageContentListParam{
						// {OfInputFile: &responses.ResponseInputFileParam{}},
						{OfInputText: &responses.ResponseInputTextParam{Text: ""}},
					},
				}},
				{OfMessage: &responses.EasyInputMessageParam{Role: "user", Content: responses.EasyInputMessageContentUnionParam{OfString: param.NewOpt(input)}}},
			},
		},
		Model: openai.ChatModelGPT5_2,
	})

	s.liveStream = session.NewStream()
	go s.handleStream(stream)
	return nil
}

func (s *Session) handleStream(stream *ssestream.Stream[responses.ResponseStreamEventUnion]) {
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
		case responses.ResponseTextDoneEvent:
			currentMessage.Contents = data.Text
			s.liveStream.MessageReplace(session.Message{
				ID:       messageId,
				Contents: data.Text,
			})
		default:
			s.liveStream.MessageNew(session.Message{
				ID:       messageId,
				Kind:     session.MKError,
				Contents: fmt.Sprintf("Unhandled stream event: %#v", data),
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
	}
}

func (s *Session) Events() <-chan domain.Event {
	panic("unimplemented")
}

func (s *Session) Kill() error {
	panic("unimplemented")
}

func (s *Session) Status() session.Status {
	panic("unimplemented")
}

func (s *Session) Stop(ctx context.Context) error {
	panic("unimplemented")
}
