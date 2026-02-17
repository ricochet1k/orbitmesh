package session

import (
	"context"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type MockSession struct{}

func (m *MockSession) Start(ctx context.Context, config Config) error    { return nil }
func (m *MockSession) Stop(ctx context.Context) error                    { return nil }
func (m *MockSession) Pause(ctx context.Context) error                   { return nil }
func (m *MockSession) Resume(ctx context.Context) error                  { return nil }
func (m *MockSession) Kill() error                                       { return nil }
func (m *MockSession) Status() Status                                    { return Status{} }
func (m *MockSession) Events() <-chan domain.Event                       { return nil }
func (m *MockSession) SendInput(ctx context.Context, input string) error { return nil }
