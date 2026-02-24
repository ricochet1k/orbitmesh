package tools

import (
	"context"
	"encoding/json"
	"log"
)

// WrapSync wraps a synchronous Handler into an AsyncHandler.
// The caller (DispatchAndWatch) already runs Run in a goroutine, so the handler
// is called directly and the eval is resolved before Run returns.
func WrapSync(h Handler) *AsyncHandler {
	return &AsyncHandler{
		Run: func(ctx context.Context, input json.RawMessage, handle EvalHandle) error {
			result, err := h(ctx, input)
			if err != nil {
				log.Printf("WrapSync err: %v", err)
				handle.Fail(err)
			} else {
				handle.Complete(result)
			}
			return nil
		},
	}
}

// WrapAtRegistration returns an AsyncHandler for a ToolDef, wrapping a sync
// Handler if needed. Returns nil if neither handler is set.
func WrapAtRegistration(def ToolDef) *AsyncHandler {
	if def.AsyncHandler != nil {
		return def.AsyncHandler
	}
	if def.Handler != nil {
		return WrapSync(def.Handler)
	}
	return nil
}
