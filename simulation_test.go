package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/heroiclabs/nakama-common/runtime"
)

func testState(t *testing.T) *State {
	t.Helper()
	if err := loadArena(); err != nil {
		t.Fatal(err)
	}
	return newState("owner")
}

func TestRegenAndDamageProtection(t *testing.T) {
	s := testState(t)
	p := &Actor{Slot: 0, HP: 100, Stamina: 100, X: world.Players[0].X, Y: world.Players[0].Y}
	s.damage(p, nil, 20)
	s.damage(p, nil, 20)
	if p.HP != 80 || p.Hit != 1 || p.DamageTotal != 20 {
		t.Fatalf("i-frames failed: %+v", p)
	}
	for i := 0; i < 5*tickRate; i++ {
		s.advance(p)
	}
	if p.HP != 80 {
		t.Fatalf("healed before delay: %d", p.HP)
	}
	for i := 0; i < tickRate; i++ {
		s.advance(p)
	}
	if p.HP != 85 {
		t.Fatalf("expected 5 HP/sec: %d", p.HP)
	}
	s.damage(p, nil, 20)
	for i := 0; i < 5*tickRate; i++ {
		s.advance(p)
	}
	if p.HP != 65 {
		t.Fatalf("damage must reset delay: %d", p.HP)
	}
	for i := 0; i < 10*tickRate; i++ {
		s.advance(p)
	}
	if p.HP != 100 {
		t.Fatalf("regen must cap at 100: %d", p.HP)
	}
}

func TestDashStaminaAndCooldown(t *testing.T) {
	s := testState(t)
	p := &Actor{Slot: 0, HP: 100, Stamina: 100, Face: 1, X: world.Players[0].X, Y: world.Players[0].Y, input: Input{Dash: true}}
	s.advance(p)
	if p.Stamina != 70 || p.dashCooldown != dashCooldownTicks {
		t.Fatalf("dash did not spend stamina: %+v", p)
	}
	s.advance(p)
	if p.Stamina < 70 || p.dashCooldown != dashCooldownTicks-1 {
		t.Fatal("cooldown did not block second dash")
	}
	p.Stamina = 0
	p.dashCooldown = 0
	p.dashTicks = 0
	s.advance(p)
	if p.dashTicks != 0 || p.dashCooldown != 0 {
		t.Fatal("dash allowed without stamina")
	}
	p.input.Dash = false
	for i := 0; i < 6*tickRate; i++ {
		s.advance(p)
	}
	if p.Stamina != 100 {
		t.Fatalf("stamina did not refill: %f", p.Stamina)
	}
}

type testPresence struct {
	runtime.Presence
	id string
}

func (p testPresence) GetUserId() string    { return p.id }
func (p testPresence) GetSessionId() string { return p.id }

type testDispatcher struct{ runtime.MatchDispatcher }

func (testDispatcher) BroadcastMessage(int64, []byte, []runtime.Presence, runtime.Presence, bool) error {
	return nil
}

func TestTenPlayersAdmissionAndRespawn(t *testing.T) {
	s := testState(t)
	m := &ArenaMatch{}
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		p := testPresence{id: fmt.Sprintf("player-%d", i)}
		_, ok, reason := m.MatchJoinAttempt(ctx, nil, nil, nil, nil, 0, s, p, nil)
		if !ok {
			t.Fatal(reason)
		}
		m.MatchJoin(ctx, nil, nil, nil, testDispatcher{}, 0, s, []runtime.Presence{p})
	}
	_, ok, _ := m.MatchJoinAttempt(ctx, nil, nil, nil, nil, 0, s, testPresence{id: "eleventh"}, nil)
	if ok {
		t.Fatal("eleventh player admitted")
	}
	_, ok, _ = m.MatchJoinAttempt(ctx, nil, nil, nil, nil, 0, s, testPresence{id: "player-1"}, nil)
	if ok {
		t.Fatal("duplicate guest admitted")
	}
	for i, p := range s.players {
		if p.Slot != i {
			t.Fatal("duplicate slot")
		}
		s.damage(p, nil, 100)
		for j := 0; j < tickRate; j++ {
			s.advance(p)
		}
		if p.HP != 100 || p.Stamina != 100 || p.Deaths != 1 {
			t.Fatalf("respawn failed for slot %d", i)
		}
	}
	m.MatchLeave(ctx, nil, nil, nil, nil, 0, s, []runtime.Presence{testPresence{id: "player-3"}})
	if freeSlot(s.players) != 3 {
		t.Fatal("vacated slot not reused")
	}
}

func TestRoomCodeValidation(t *testing.T) {
	for _, code := range []string{"0000", "1234", "9999"} {
		if !validRoomCode(code) {
			t.Fatal(code)
		}
	}
	for _, code := range []string{"", "123", "12345", "12a4", "１２３４", " 123"} {
		if validRoomCode(code) {
			t.Fatal(code)
		}
	}
}

func TestAllPlayersCountInResults(t *testing.T) {
	s := testState(t)
	s.players = []*Actor{{ID: "one", Deaths: 2}, {ID: "two", Deaths: 1}, {ID: "three", Deaths: 0, Kills: 4}}
	s.finish(100)
	if s.Result != "three wins" {
		t.Fatal(s.Result)
	}
	s.players = append(s.players, &Actor{ID: "four", Kills: 4})
	s.finish(100)
	if s.Result != "Draw" {
		t.Fatal(s.Result)
	}
}
