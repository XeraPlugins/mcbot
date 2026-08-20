package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Stats struct {
	cfg *Config

	attempts atomic.Int64
	connected atomic.Int64
	failures  atomic.Int64
	active    atomic.Int64 // currently in-game

	mu       sync.Mutex
	dropped  int64 // dropped after being in-game
	lastErrs map[string]int

	start time.Time
}

func NewStats(cfg *Config) *Stats {
	return &Stats{
		cfg:      cfg,
		lastErrs: make(map[string]int),
		start:    time.Now(),
	}
}

func (s *Stats) Connecting(name string) {
	s.attempts.Add(1)
}

func (s *Stats) Failed(name, err string) {
	s.failures.Add(1)
	s.mu.Lock()
	s.lastErrs[err]++
	s.mu.Unlock()
}

func (s *Stats) Connected(name string) {
	s.connected.Add(1)
	s.active.Add(1)
}

func (s *Stats) Disconnected(name, err string) {
	s.active.Add(-1)
	s.mu.Lock()
	s.dropped++
	s.mu.Unlock()
}

func (s *Stats) snapshot() (attempts, connected, failures, active, dropped int64, topErrs []string) {
	attempts = s.attempts.Load()
	connected = s.connected.Load()
	failures = s.failures.Load()
	active = s.active.Load()
	s.mu.Lock()
	dropped = s.dropped
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range s.lastErrs {
		list = append(list, kv{k, v})
	}
	s.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	topErrs = make([]string, 0, min(3, len(list)))
	for i, it := range list {
		if i >= 3 {
			break
		}
		topErrs = append(topErrs, fmt.Sprintf("%s(x%d)", it.k, it.v))
	}
	return
}

// Snapshot is a JSON-friendly view of the current stats.
type Snapshot struct {
	Running   bool     `json:"running"`
	Address   string   `json:"address"`
	Elapsed   string   `json:"elapsed"`
	Attempts  int64    `json:"attempts"`
	Connected int64    `json:"connected"`
	Failures  int64    `json:"failures"`
	Active    int64    `json:"active"`
	Dropped   int64    `json:"dropped"`
	SuccessPct float64 `json:"successPct"`
	TopErrs   []string `json:"topErrs"`
}

func (s *Stats) Snapshot(running bool, addr string) Snapshot {
	attempts, connected, failures, active, dropped, topErrs := s.snapshot()
	successPct := 0.0
	if attempts > 0 {
		successPct = float64(connected) / float64(attempts) * 100
	}
	return Snapshot{
		Running:    running,
		Address:    addr,
		Elapsed:    time.Since(s.start).Round(time.Second).String(),
		Attempts:   attempts,
		Connected:  connected,
		Failures:   failures,
		Active:     active,
		Dropped:    dropped,
		SuccessPct: successPct,
		TopErrs:    topErrs,
	}
}

func (s *Stats) Print(addr string, port uint16) {	attempts, connected, failures, active, dropped, topErrs := s.snapshot()
	elapsed := time.Since(s.start).Round(time.Second)

	successRate := 0.0
	if attempts > 0 {
		successRate = float64(connected) / float64(attempts) * 100
	}

	line := fmt.Sprintf("\r[%s] %s:%d | in-game: %d/%d | joined: %d | failed: %d | dropped: %d | success: %.1f%%",
		elapsed, addr, port, active, attempts, connected, failures, dropped, successRate)

	if len(topErrs) > 0 {
		line += " | top errs: " + topErrs[0]
		for _, e := range topErrs[1:] {
			line += ", " + e
		}
	}
	fmt.Print(line + "   ")
}

func (s *Stats) Final(addr string, port uint16) {
	attempts, connected, failures, active, dropped, _ := s.snapshot()
	elapsed := time.Since(s.start).Round(time.Second)

	fmt.Printf("\n\n--- results after %s against %s:%d ---\n", elapsed, addr, port)
	fmt.Printf("attempts:  %d\n", attempts)
	fmt.Printf("joined:    %d\n", connected)
	fmt.Printf("failed:    %d\n", failures)
	fmt.Printf("in-game:   %d\n", active)
	fmt.Printf("dropped:   %d\n", dropped)
	if attempts > 0 {
		fmt.Printf("success:   %.1f%%\n", float64(connected)/float64(attempts)*100)
	}
}
