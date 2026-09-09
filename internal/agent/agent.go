// Package agent defines the subprocess boundary used by the application. It has
// no knowledge of windows, model credentials or the application's history index.
package agent

import (
	"context"
	"errors"
)

type Config struct {
	Executable  string
	CWD         string
	SessionDir  string
	SessionFile string
	ExtraArgs   []string
}

type Client interface {
	// Request sends one RPC command. A failed RPC response is returned alongside
	// an error. Cancellation stops waiting; it does not retract an accepted command.
	// extension_ui_response is one-way and preserves the supplied interaction id.
	Request(context.Context, map[string]any) (map[string]any, error)
	// Responses may include adapter-only _eventSequence (uint64), the number of
	// prior async events read before the response. Events carry _sequence (uint64),
	// enabling the consumer to await event application before reconciling state.
	// Events must be consumed for the client's lifetime. The channel closes after
	// process_exit, including when Close was requested by the application.
	Events() <-chan map[string]any
	Close() error
}

type Factory interface {
	Start(context.Context, Config) (Client, error)
}

// HistoryReader optionally provides a lightweight display projection from the
// agent's own persisted history. It must not rebuild or change model context.
type HistoryReader interface {
	ReadHistory(context.Context) ([]map[string]any, error)
}

// ErrHistoryMissing distinguishes a not-yet-persisted new conversation from lost
// agent state. The application must not silently resume existing history fresh.
var ErrHistoryMissing = errors.New("Pi 会话文件已不存在，无法恢复已有上下文；请新建会话")
