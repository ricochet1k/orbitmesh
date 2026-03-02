package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
)

const defaultProcessIdleTTL = 3 * time.Minute

const (
	acpInitializeTimeout  = 11 * time.Second
	acpSessionOpenTimeout = 8 * time.Second
)

var sharedRuntimePool = newRuntimePool(defaultProcessIdleTTL)

type runtimePool struct {
	mu      sync.Mutex
	idleTTL time.Duration
	items   map[string]*sharedRuntime
}

type RuntimePoolStats struct {
	RuntimeCount   int   `json:"runtime_count"`
	ActiveSessions int   `json:"active_sessions"`
	IdleRuntimes   int   `json:"idle_runtimes"`
	IdleTTLSeconds int64 `json:"idle_ttl_seconds"`
}

func newRuntimePool(idleTTL time.Duration) *runtimePool {
	return &runtimePool{
		idleTTL: idleTTL,
		items:   make(map[string]*sharedRuntime),
	}
}

func (p *runtimePool) acquire(ctx context.Context, cfg process.Config) (*sharedRuntime, error) {
	key := runtimeKey(cfg)

	p.mu.Lock()
	if rt, ok := p.items[key]; ok {
		p.mu.Unlock()
		rt.cancelIdleShutdown()
		return rt, nil
	}
	p.mu.Unlock()

	rt, err := newSharedRuntime(ctx, key, cfg, p)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if existing, ok := p.items[key]; ok {
		p.mu.Unlock()
		_ = rt.shutdown()
		existing.cancelIdleShutdown()
		return existing, nil
	}
	p.items[key] = rt
	p.mu.Unlock()

	return rt, nil
}

func (p *runtimePool) remove(key string, rt *sharedRuntime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.items[key]; ok && existing == rt {
		delete(p.items, key)
	}
}

func (p *runtimePool) stats() RuntimePoolStats {
	p.mu.Lock()
	runtimes := make([]*sharedRuntime, 0, len(p.items))
	for _, rt := range p.items {
		runtimes = append(runtimes, rt)
	}
	ttl := p.idleTTL
	p.mu.Unlock()

	stats := RuntimePoolStats{RuntimeCount: len(runtimes), IdleTTLSeconds: int64(ttl.Seconds())}
	for _, rt := range runtimes {
		rt.mu.RLock()
		sessionCount := len(rt.sessions)
		rt.mu.RUnlock()
		stats.ActiveSessions += sessionCount
		if sessionCount == 0 {
			stats.IdleRuntimes++
		}
	}

	return stats
}

func SharedRuntimeStats() RuntimePoolStats {
	return sharedRuntimePool.stats()
}

type sharedRuntime struct {
	key     string
	pool    *runtimePool
	process *process.Manager
	conn    *acpsdk.ClientSideConnection
	ctx     context.Context
	cancel  context.CancelFunc
	idleTTL time.Duration
	adapter *acpClientAdapter

	mu        sync.RWMutex
	sessions  map[string]*Session
	idleTimer *time.Timer
	stopped   bool

	shutdownOnce sync.Once
}

func newSharedRuntime(initCtx context.Context, key string, cfg process.Config, pool *runtimePool) (*sharedRuntime, error) {
	ctx, cancel := context.WithCancel(context.Background())

	mgr, err := process.Start(ctx, cfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start acp agent process: %w", err)
	}

	rt := &sharedRuntime{
		key:      key,
		pool:     pool,
		process:  mgr,
		ctx:      ctx,
		cancel:   cancel,
		idleTTL:  pool.idleTTL,
		sessions: make(map[string]*Session),
	}
	rt.adapter = newACPClientAdapter(rt)
	rt.conn = acpsdk.NewClientSideConnection(rt.adapter, mgr.Stdin(), mgr.Stdout())

	if err := rt.initialize(initCtx); err != nil {
		_ = mgr.Kill()
		cancel()
		return nil, err
	}

	go rt.processStderr()
	go rt.waitForExit()
	rt.scheduleIdleShutdown()

	return rt, nil
}

func (rt *sharedRuntime) initialize(ctx context.Context) error {
	if ctx == nil {
		ctx = rt.ctx
	}
	initCtx := ctx
	if _, hasDeadline := initCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		initCtx, cancel = context.WithTimeout(ctx, acpInitializeTimeout)
		defer cancel()
	}

	initReq := acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{
			Fs: acpsdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
	}
	if _, err := rt.conn.Initialize(initCtx, initReq); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}
	return nil
}

func (rt *sharedRuntime) createSession(ctx context.Context, s *Session, cfg sessionRuntimeConfig) (string, error) {
	rt.cancelIdleShutdown()
	if ctx == nil {
		ctx = context.Background()
	}
	openCtx := ctx
	if _, hasDeadline := openCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		openCtx, cancel = context.WithTimeout(ctx, acpSessionOpenTimeout)
		defer cancel()
	}

	resp, err := rt.conn.NewSession(openCtx, acpsdk.NewSessionRequest{
		Cwd:        cfg.cwd,
		McpServers: cfg.mcpServers,
	})
	if err != nil {
		return "", fmt.Errorf("new session failed: %w", err)
	}

	acpSessionID := string(resp.SessionId)
	rt.mu.Lock()
	if rt.stopped {
		rt.mu.Unlock()
		return "", fmt.Errorf("acp runtime has stopped")
	}
	rt.sessions[acpSessionID] = s
	rt.mu.Unlock()

	return acpSessionID, nil
}

func (rt *sharedRuntime) removeSession(acpSessionID string) {
	if acpSessionID == "" {
		return
	}
	rt.mu.Lock()
	delete(rt.sessions, acpSessionID)
	remaining := len(rt.sessions)
	rt.mu.Unlock()

	if remaining == 0 {
		rt.scheduleIdleShutdown()
	}
}

func (rt *sharedRuntime) sessionForID(acpSessionID string) *Session {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.sessions[acpSessionID]
}

func (rt *sharedRuntime) broadcastMetadata(key string, value any) {
	rt.mu.RLock()
	sessions := make([]*Session, 0, len(rt.sessions))
	for _, sess := range rt.sessions {
		sessions = append(sessions, sess)
	}
	rt.mu.RUnlock()

	for _, sess := range sessions {
		sess.events.Emit(domain.NewMetadataEvent(sess.sessionID, key, value, nil))
	}
}

func (rt *sharedRuntime) processStderr() {
	if rt.process == nil || rt.process.Stderr() == nil {
		return
	}

	scanner := bufio.NewScanner(rt.process.Stderr())
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		rt.broadcastMetadata("stderr", map[string]any{"line": line})
	}
}

func (rt *sharedRuntime) waitForExit() {
	if rt.process != nil {
		_ = rt.process.Wait()
	}

	rt.mu.Lock()
	if rt.stopped {
		rt.mu.Unlock()
		return
	}
	rt.stopped = true
	sessions := make([]*Session, 0, len(rt.sessions))
	for _, sess := range rt.sessions {
		sessions = append(sessions, sess)
	}
	rt.sessions = make(map[string]*Session)
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
		rt.idleTimer = nil
	}
	rt.mu.Unlock()

	for _, sess := range sessions {
		sess.handleRuntimeStopped()
	}

	rt.pool.remove(rt.key, rt)
}

func (rt *sharedRuntime) scheduleIdleShutdown() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.stopped {
		return
	}
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
	}
	rt.idleTimer = time.AfterFunc(rt.idleTTL, func() {
		rt.shutdownIfIdle()
	})
}

func (rt *sharedRuntime) cancelIdleShutdown() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
		rt.idleTimer = nil
	}
}

func (rt *sharedRuntime) shutdownIfIdle() {
	rt.mu.RLock()
	idle := !rt.stopped && len(rt.sessions) == 0
	rt.mu.RUnlock()
	if idle {
		_ = rt.shutdown()
	}
}

func (rt *sharedRuntime) shutdown() error {
	var err error
	rt.shutdownOnce.Do(func() {
		rt.mu.Lock()
		if rt.stopped {
			rt.mu.Unlock()
			return
		}
		rt.stopped = true
		if rt.idleTimer != nil {
			rt.idleTimer.Stop()
			rt.idleTimer = nil
		}
		rt.mu.Unlock()

		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.process != nil {
			err = rt.process.Stop(5 * time.Second)
		}
		if rt.pool != nil {
			rt.pool.remove(rt.key, rt)
		}
	})
	return err
}

func (rt *sharedRuntime) cancelPrompt(acpSessionID string) {
	if acpSessionID == "" {
		return
	}
	_ = rt.conn.Cancel(context.Background(), acpsdk.CancelNotification{SessionId: acpsdk.SessionId(acpSessionID)})
}

type sessionRuntimeConfig struct {
	cwd        string
	mcpServers []acpsdk.McpServer
}

func runtimeKey(cfg process.Config) string {
	args := strings.Join(cfg.Args, "\x00")
	envPairs := make([]string, 0, len(cfg.Environment))
	for k, v := range cfg.Environment {
		envPairs = append(envPairs, k+"="+v)
	}
	sort.Strings(envPairs)

	payload := map[string]any{
		"cmd":  cfg.Command,
		"args": args,
		"cwd":  cfg.WorkingDir,
		"env":  envPairs,
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func mergeEnvironment(base map[string]string, extra map[string]string) map[string]string {
	env := maps.Clone(base)
	maps.Copy(env, extra)
	return env
}
