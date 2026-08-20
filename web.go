package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

type webServer struct {
	runner *Runner
	mux    *http.ServeMux
}

func NewWebServer(runner *Runner) *webServer {
	w := &webServer{runner: runner, mux: http.NewServeMux()}

	w.mux.HandleFunc("/", w.handleIndex)
	w.mux.HandleFunc("/api/start", w.handleStart)
	w.mux.HandleFunc("/api/stop", w.handleStop)
	w.mux.HandleFunc("/api/stats", w.handleStats)
	return w
}

func (w *webServer) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("web panic: %v", rec)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
		}
	}()
	w.mux.ServeHTTP(rw, r)
}

func (w *webServer) handleIndex(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Write(indexHTML)
}

// webConfig mirrors Config but with plain units so it round-trips through JSON.
type webConfig struct {
	Address      string  `json:"address"`
	Bots         int     `json:"bots"`
	Rate         int     `json:"rate"`
	Stagger      int     `json:"stagger"` // ms
	Prefix       string  `json:"prefix"`
	NamesFile    string  `json:"namesFile"`
	Move         bool    `json:"move"`
	Tick         int     `json:"tick"` // ms
	Radius       float64 `json:"radius"`
	JoinTimeout  int     `json:"joinTimeout"` // seconds
	Duration     int     `json:"duration"`    // seconds
	User         string  `json:"user"`
	Pass         string  `json:"pass"`
	Register     bool    `json:"register"`
	AuthDelay    int     `json:"authDelay"` // ms
	Reconnect    int     `json:"reconnect"` // seconds
	MaxReconnects int    `json:"maxReconnects"`
	Decorate     bool    `json:"decorate"`
	Hide         bool    `json:"hide"`
}

func (wc *webConfig) toConfig() *Config {
	cfg := &Config{
		Address:       wc.Address,
		Bots:          wc.Bots,
		Rate:          wc.Rate,
		Stagger:       time.Duration(wc.Stagger) * time.Millisecond,
		Prefix:        wc.Prefix,
		NamesFile:     wc.NamesFile,
		Move:          wc.Move,
		Tick:          time.Duration(wc.Tick) * time.Millisecond,
		Radius:        wc.Radius,
		JoinTimeout:   time.Duration(wc.JoinTimeout) * time.Second,
		Duration:      time.Duration(wc.Duration) * time.Second,
		Interval:      time.Second,
		Quiet:         true, // web UI shows its own logging
		User:          wc.User,
		Pass:          wc.Pass,
		Register:      wc.Register,
		AuthDelay:     time.Duration(wc.AuthDelay) * time.Millisecond,
		ReconnectDelay: time.Duration(wc.Reconnect) * time.Second,
		MaxReconnects: wc.MaxReconnects,
		Decorate:      wc.Decorate,
		Hide:          wc.Hide,
	}
	if wc.Decorate {
		cfg.Move = false
		if cfg.ReconnectDelay <= 0 {
			cfg.ReconnectDelay = 10 * time.Second
		}
	}
	return cfg
}

func (w *webServer) handleStart(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var wc webConfig
	if err := json.NewDecoder(r.Body).Decode(&wc); err != nil {
		http.Error(rw, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if wc.Address == "" {
		wc.Address = "127.0.0.1:25565"
	}
	if err := w.runner.Start(wc.toConfig()); err != nil {
		http.Error(rw, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(rw, map[string]any{"ok": true})
}

func (w *webServer) handleStop(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.runner.Stop()
	writeJSON(rw, map[string]any{"ok": true})
}

func (w *webServer) handleStats(rw http.ResponseWriter, r *http.Request) {
	stats := w.runner.Stats()
	running := w.runner.Running()
	addr := ""
	if stats == nil {
		// No run started yet; report an empty snapshot.
		snap := Snapshot{Running: running, TopErrs: []string{}}
		writeJSON(rw, snap)
		return
	}
	addr = stats.cfg.Address
	snap := stats.Snapshot(running, addr)
	writeJSON(rw, snap)
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(v)
}

func runWebServer(runner *Runner, listen string) error {
	srv := &http.Server{
		Addr:    listen,
		Handler: NewWebServer(runner),
	}
	log.Printf("web UI listening on http://%s", listen)
	return srv.ListenAndServe()
}
