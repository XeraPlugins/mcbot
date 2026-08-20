package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Tnze/go-mc/bot"
)

// Runner owns a single stress-test run: it spawns bots, tracks their stats,
// and can be started, stopped, and inspected (used by both the CLI and web UI).
type Runner struct {
	mu      sync.Mutex
	running bool
	cfg     *Config
	stats   *Stats
	ctx     context.Context
	cancel  context.CancelFunc
	reg     *ClientRegistry
	wg      sync.WaitGroup
}

func NewRunner() *Runner {
	return &Runner{}
}

// Start launches a new run. Returns an error if one is already running.
func (r *Runner) Start(cfg *Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return fmt.Errorf("a run is already in progress")
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cfg = cfg
	r.stats = NewStats(cfg)
	r.ctx = ctx
	r.cancel = cancel
	r.reg = NewClientRegistry()

	if cfg.NamesFile != "" {
		if err := writeNamesFile(cfg.NamesFile, cfg.Bots); err != nil {
			cancel()
			return fmt.Errorf("write names file: %w", err)
		}
	}

	// Self-terminating mode.
	if cfg.Duration > 0 {
		go func() {
			select {
			case <-time.After(cfg.Duration):
				r.Stop()
			case <-ctx.Done():
			}
		}()
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.spawn(ctx, cfg)
	}()
	r.running = true
	return nil
}

func (r *Runner) spawn(ctx context.Context, cfg *Config) {
	spawnDelay := time.Duration(0)
	if cfg.Rate > 0 {
		spawnDelay = time.Second / time.Duration(cfg.Rate)
	}
	if cfg.Stagger > 0 {
		spawnDelay = cfg.Stagger
	}
	for i := 0; i < cfg.Bots; i++ {
		if ctx.Err() != nil {
			return
		}
		name := makeName(i)
		if cfg.Prefix != "" && cfg.Prefix != "Bot" {
			name = fmt.Sprintf("%s%03d", cfg.Prefix, i)
		}
		r.wg.Add(1)
		go func(name string) {
			defer r.wg.Done()
			runBot(ctx, cfg, r.stats, r.reg, name)
		}(name)
		if spawnDelay > 0 && i < cfg.Bots-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(spawnDelay):
			}
		}
	}
}

// Stop cancels the run and force-closes all live connections.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.cancel()
	if r.reg != nil {
		r.reg.CloseAll()
	}
}

// Running reports whether a run is active.
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Stats returns the stats object of the current/previous run.
func (r *Runner) Stats() *Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// Wait blocks until the current run's bots have all finished.
func (r *Runner) Wait() {
	r.wg.Wait()
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()
}

// Done returns a channel closed when the run is cancelled.
func (r *Runner) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil {
		return nil
	}
	return r.ctx.Done()
}

// ClientRegistry tracks every live bot connection so a run can be stopped
// promptly instead of waiting for keep-alive timeouts.
type ClientRegistry struct {
	mu sync.Mutex
	m  map[*bot.Client]struct{}
}

func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{m: make(map[*bot.Client]struct{})}
}

func (c *ClientRegistry) Add(cl *bot.Client) {
	c.mu.Lock()
	c.m[cl] = struct{}{}
	c.mu.Unlock()
}

func (c *ClientRegistry) Remove(cl *bot.Client) {
	c.mu.Lock()
	delete(c.m, cl)
	c.mu.Unlock()
}

func (c *ClientRegistry) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for cl := range c.m {
		cl.Close()
	}
}
