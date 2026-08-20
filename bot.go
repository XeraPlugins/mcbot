package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log"
	"math"
	crand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/Tnze/go-mc/bot"
	"github.com/Tnze/go-mc/bot/basic"
	"github.com/Tnze/go-mc/chat"
	"github.com/Tnze/go-mc/chat/sign"
	"github.com/Tnze/go-mc/data/packetid"
	pk "github.com/Tnze/go-mc/net/packet"
)

type Bot struct {
	id     int
	name   string
	client *bot.Client
	player *basic.Player

	registered bool // has this bot completed /register at least once

	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	mu       sync.Mutex
	x, y, z  float64
	yaw      float32
	grounded bool
	hasPos   bool

	targetDX, targetDZ float64

	stats *Stats
	cfg   *Config
}

// runBot maintains one bot's lifecycle, reconnecting as configured.
func runBot(ctx context.Context, cfg *Config, stats *Stats, reg *ClientRegistry, name string) {
	b := &Bot{
		name:  name,
		stats: stats,
		cfg:   cfg,
	}

	reconnects := 0
	for {
		if ctx.Err() != nil {
			return
		}

		// Each connection gets a fresh client and its own session context so a
		// stale wander loop from the previous connection can't write into the
		// new one.
		sessionCtx, sessionCancel := context.WithCancel(ctx)
		b.sessionCtx = sessionCtx
		b.sessionCancel = sessionCancel
		b.client = bot.NewClient()
		b.client.Auth.Name = name
		if reg != nil {
			reg.Add(b.client)
		}

		b.player = basic.NewPlayer(b.client, basic.DefaultSettings, basic.EventsListener{
			GameStart:  b.onGameStart,
			Disconnect: b.onDisconnect,
			Teleported: b.onTeleported,
		})

		stats.Connecting(name)
		joinCtx, cancel := context.WithTimeout(sessionCtx, cfg.JoinTimeout)
		err := b.client.JoinServerWithOptions(cfg.Address, bot.JoinOptions{Context: joinCtx})
		cancel()

		if err != nil {
			sessionCancel()
			if reg != nil {
				reg.Remove(b.client)
			}
			stats.Failed(name, err.Error())
			if !cfg.Quiet {
				log.Printf("[%s] join failed: %v", name, err)
			}
			if !b.shouldReconnect(reconnects) {
				return
			}
			reconnects++
			if !sleepCtx(ctx, cfg.ReconnectDelay) {
				return
			}
			continue
		}

		stats.Connected(name)
		if !cfg.Quiet {
			log.Printf("[%s] joined", name)
		}

		// HandleGame blocks until the connection drops or context is cancelled.
		err = b.client.HandleGame()
		sessionCancel()
		b.client.Close()
		if reg != nil {
			reg.Remove(b.client)
		}
		stats.Disconnected(name, err.Error())
		if !cfg.Quiet {
			log.Printf("[%s] disconnected: %v", name, err)
		}

		if !b.shouldReconnect(reconnects) {
			return
		}
		reconnects++
		if !sleepCtx(ctx, cfg.ReconnectDelay) {
			return
		}
	}
}

func (b *Bot) shouldReconnect(reconnects int) bool {
	if b.cfg.ReconnectDelay <= 0 {
		return false
	}
	if b.cfg.MaxReconnects > 0 && reconnects >= b.cfg.MaxReconnects {
		return false
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// onGameStart runs once the bot is in-game: auth, then movement behavior.
func (b *Bot) onGameStart() error {
	if b.cfg.User != "" {
		if b.cfg.Register && !b.registered {
			go b.safe(b.doAuth)
		} else {
			go b.safe(b.doLogin)
		}
	}
	switch {
	case b.cfg.Decorate && b.cfg.Hide:
		go b.safe(b.flyHigh)
	case b.cfg.Decorate:
		// idle: stay put, just keep the connection alive
	case b.cfg.Move:
		go b.safe(b.wander)
	}
	return nil
}

// safe runs fn in a goroutine, swallowing panics that occur when the
// connection is torn down mid-write (go-mc panics on a closed queue).
func (b *Bot) safe(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			if !b.cfg.Quiet {
				log.Printf("[%s] recovered: %v", b.name, r)
			}
		}
	}()
	fn()
}

// closed reports whether this connection's session is over, so writes can
// stop before hitting the closed queue.
func (b *Bot) closed() bool {
	return b.sessionCtx == nil || b.sessionCtx.Err() != nil
}

// doAuth runs the first-login flow: /cracked then /register <user> <pass>.
func (b *Bot) doAuth() {
	time.Sleep(b.cfg.AuthDelay)
	if err := b.sendCommand("/cracked"); err != nil {
		if !b.cfg.Quiet {
			log.Printf("[%s] auth: %v", b.name, err)
		}
		return
	}
	time.Sleep(b.cfg.AuthDelay)
	if err := b.sendCommand("/register " + b.cfg.User + " " + b.cfg.Pass); err != nil {
		if !b.cfg.Quiet {
			log.Printf("[%s] auth: %v", b.name, err)
		}
		return
	}
	b.registered = true
}

// doLogin runs /login <user> for already-registered bots.
func (b *Bot) doLogin() {
	time.Sleep(b.cfg.AuthDelay)
	if err := b.sendCommand("/login " + b.cfg.User); err != nil && !b.cfg.Quiet {
		log.Printf("[%s] auth: %v", b.name, err)
	}
}

// sendCommand writes a chat command packet (the leading "/" is stripped, as
// the protocol expects the raw command).
func (b *Bot) sendCommand(cmd string) error {
	if b.closed() {
		return errors.New("connection closed")
	}
	cmd = strings.TrimPrefix(cmd, "/")

	var salt int64
	if err := binary.Read(rand.Reader, binary.BigEndian, &salt); err != nil {
		return err
	}

	return b.client.Conn.WritePacket(pk.Marshal(
		packetid.ServerboundChatCommand,
		pk.String(cmd),
		pk.Long(time.Now().UnixMilli()),
		pk.Long(salt),
		pk.Ary[pk.VarInt]{Ary: []pk.Tuple{}},
		sign.HistoryUpdate{Acknowledged: pk.NewFixedBitSet(20)},
	))
}

// onTeleported syncs our tracked position and confirms the teleport so the
// server keeps accepting movement packets.
func (b *Bot) onTeleported(x, y, z float64, yaw, pitch float32, _ byte, teleportID int32) error {
	b.mu.Lock()
	b.x, b.y, b.z = x, y, z
	b.yaw = yaw
	b.hasPos = true
	b.mu.Unlock()

	return b.player.AcceptTeleportation(pk.VarInt(teleportID))
}

func (b *Bot) onDisconnect(reason chat.Message) error {
	return errors.New("server disconnect: " + reason.ClearString())
}

// wander moves the bot with a random walk confined to a radius around spawn.
// It stops when the session context is cancelled (connection lost/shutdown).
func (b *Bot) wander() {
	rng := crand.New(crand.NewSource(time.Now().UnixNano() + int64(b.name[0])))
	sessionCtx := b.sessionCtx

	// Wait until we've got a spawn position from the teleport packet.
	for {
		b.mu.Lock()
		hasPos := b.hasPos
		b.mu.Unlock()
		if hasPos {
			break
		}
		select {
		case <-sessionCtx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	b.mu.Lock()
	centerX, centerZ := b.x, b.z
	b.mu.Unlock()

	tick := time.NewTicker(b.cfg.Tick)
	defer tick.Stop()

	step := 0
	for {
		select {
		case <-sessionCtx.Done():
			return
		case <-tick.C:
		}

		b.mu.Lock()
		x, y, z, grounded := b.x, b.y, b.z, b.grounded
		b.mu.Unlock()

		// Pick a new random direction every few ticks.
		if step <= 0 {
			step = rng.Intn(10) + 2
			dx, dz := rng.Float64()*2-1, rng.Float64()*2-1
			// If stepping would exit the radius, point back at the center.
			if math.Hypot(x+dx-centerX, z+dz-centerZ) > b.cfg.Radius {
				dx, dz = centerX-x, centerZ-z
				if dl := math.Hypot(dx, dz); dl > 0 {
					dx, dz = dx/dl, dz/dl
				}
			}
			b.mu.Lock()
			b.targetDX, b.targetDZ = dx, dz
			b.mu.Unlock()
		}
		step--

		b.mu.Lock()
		dx, dz := b.targetDX, b.targetDZ
		b.mu.Unlock()

		nx := x + dx
		nz := z + dz

		// yaw so the head points where we walk.
		yaw := float32(math.Atan2(-dx, -dz) * 180 / math.Pi)

		if b.closed() {
			return
		}
		err := b.client.Conn.WritePacket(pk.Marshal(
			packetid.ServerboundMovePlayerPosRot,
			pk.Double(nx),
			pk.Double(y),
			pk.Double(nz),
			pk.Float(yaw),
			pk.Float(0),
			pk.Boolean(grounded),
		))
		if err != nil {
			return
		}

		b.mu.Lock()
		b.x, b.z = nx, nz
		b.yaw = yaw
		b.mu.Unlock()
	}
}

// flyHigh lifts the bot above the world's build limit so players can't see it,
// then keeps it there. The player count still includes it, which is what makes
// the server look busier than it is.
func (b *Bot) flyHigh() {
	// Wait until we've got a spawn position from the teleport packet.
	for {
		b.mu.Lock()
		hasPos := b.hasPos
		b.mu.Unlock()
		if hasPos {
			break
		}
		select {
		case <-b.sessionCtx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	b.mu.Lock()
	x, z := b.x, b.z
	b.mu.Unlock()

	const targetY = 4096.0 // well above the build limit; nothing renders up here
	tick := time.NewTicker(b.cfg.Tick)
	defer tick.Stop()

	// Ascend in chunks so vanilla's "moved too quickly" check doesn't kick us.
	y := targetY
	for y >= 0 {
		select {
		case <-b.sessionCtx.Done():
			return
		case <-tick.C:
		}

		if b.closed() {
			return
		}
		err := b.client.Conn.WritePacket(pk.Marshal(
			packetid.ServerboundMovePlayerPos,
			pk.Double(x),
			pk.Double(y),
			pk.Double(z),
			pk.Boolean(false),
		))
		if err != nil {
			return
		}

		b.mu.Lock()
		b.y = y
		b.mu.Unlock()
		y -= 512
	}

	// Hold position once up there.
	for {
		select {
		case <-b.sessionCtx.Done():
			return
		case <-tick.C:
		}
		if b.closed() {
			return
		}
		if err := b.client.Conn.WritePacket(pk.Marshal(
			packetid.ServerboundMovePlayerPos,
			pk.Double(x),
			pk.Double(targetY),
			pk.Double(z),
			pk.Boolean(false),
		)); err != nil {
			return
		}
	}
}
