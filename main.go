package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	Address      string
	Bots         int
	Rate         int
	Prefix       string
	Move         bool
	Tick         time.Duration
	Radius       float64
	JoinTimeout  time.Duration
	Duration     time.Duration
	Interval     time.Duration
	Quiet        bool

	User     string
	Pass     string
	Register bool

	Stagger        time.Duration
	AuthDelay      time.Duration
	ReconnectDelay time.Duration
	MaxReconnects  int

	NamesFile string

	Web    string

	Decorate bool
	Hide    bool
}

func parseFlags() Config {
	var cfg Config
	var tickMs, joinTimeout, duration, interval int
	var staggerMs, reconnectSec, authDelayMs int

	flag.StringVar(&cfg.Web, "web", "", "start the web UI and listen on this address (e.g. :8080)")
	flag.StringVar(&cfg.Address, "address", "127.0.0.1:25565", "server address host:port")
	flag.IntVar(&cfg.Bots, "bots", 100, "total number of bots to spawn")
	flag.IntVar(&cfg.Rate, "rate", 20, "bots spawned per second (0 = all at once)")
	flag.IntVar(&staggerMs, "stagger", 0, "ms between bot joins, overrides -rate (0 = use -rate)")
	flag.StringVar(&cfg.Prefix, "prefix", "", "bot name prefix; empty uses realistic generated names")
	flag.StringVar(&cfg.NamesFile, "names-file", "", "write generated usernames to this file (e.g. names.txt)")
	flag.BoolVar(&cfg.Move, "move", true, "make bots wander around spawn")
	flag.IntVar(&tickMs, "tick", 50, "movement tick in milliseconds")
	flag.Float64Var(&cfg.Radius, "radius", 10, "movement radius around spawn")
	flag.IntVar(&joinTimeout, "join-timeout", 30, "seconds to wait for a bot to join")
	flag.IntVar(&duration, "duration", 0, "run for N seconds then stop (0 = until interrupted)")
	flag.IntVar(&interval, "interval", 1, "status refresh interval in seconds")
	flag.BoolVar(&cfg.Quiet, "quiet", false, "suppress per-bot log lines")

	flag.StringVar(&cfg.User, "user", "User7364", "auth username for /register and /login")
	flag.StringVar(&cfg.Pass, "pass", "User7364", "auth password for /register")
	flag.BoolVar(&cfg.Register, "register", true, "run /cracked + /register on first login (false = always /login)")
	flag.IntVar(&authDelayMs, "auth-delay", 500, "ms to wait after login before sending auth commands")

	flag.IntVar(&reconnectSec, "reconnect", 0, "seconds to wait before reconnecting a dropped bot (0 = no reconnect)")
	flag.IntVar(&cfg.MaxReconnects, "max-reconnects", 0, "max reconnect attempts per bot (0 = unlimited)")

	flag.BoolVar(&cfg.Decorate, "decorate", false, "make the server look populated: bots join, stay idle, and stay online")
	flag.BoolVar(&cfg.Hide, "hide", false, "with -decorate, fly bots above the world so players can't see them")

	flag.Parse()

	// Decorate mode overrides a few things: no wandering, and keep bots online
	// by defaulting reconnect to a gentle interval.
	if cfg.Decorate {
		cfg.Move = false
		if reconnectSec == 0 {
			cfg.ReconnectDelay = 10 * time.Second
		}
	}

	cfg.Stagger = time.Duration(staggerMs) * time.Millisecond
	cfg.AuthDelay = time.Duration(authDelayMs) * time.Millisecond
	cfg.ReconnectDelay = time.Duration(reconnectSec) * time.Second
	cfg.Tick = time.Duration(tickMs) * time.Millisecond
	cfg.JoinTimeout = time.Duration(joinTimeout) * time.Second
	cfg.Duration = time.Duration(duration) * time.Second
	cfg.Interval = time.Duration(interval) * time.Second
	return cfg
}

func main() {
	cfg := parseFlags()

	// Web UI mode: the CLI flags set defaults that get served to the page.
	if cfg.Web != "" {
		runner := NewRunner()
		if err := runWebServer(runner, cfg.Web); err != nil {
			fmt.Fprintln(os.Stderr, "web server:", err)
			os.Exit(1)
		}
		return
	}

	addr, port := splitAddress(cfg.Address)

	runner := NewRunner()
	if err := runner.Start(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Status reporter.
	go func() {
		t := time.NewTicker(cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-runner.Done():
				return
			case <-t.C:
				runner.Stats().Print(addr, port)
			}
		}
	}()

	// Shutdown on SIGINT/SIGTERM.
	go func() {
		<-sig
		fmt.Println("\n[shutdown] received interrupt, disconnecting bots...")
		runner.Stop()
	}()

	runner.Wait()
	runner.Stats().Final(addr, port)
}

func splitAddress(addr string) (string, uint16) {
	if h, p, err := netSplit(addr); err == nil {
		return h, p
	}
	return addr, 25565
}

func netSplit(addr string) (string, uint16, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, 0, fmt.Errorf("no port")
	}
	port, err := strconv.ParseUint(addr[idx+1:], 10, 16)
	if err != nil {
		return addr, 0, err
	}
	return addr[:idx], uint16(port), nil
}
