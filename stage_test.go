package main

import (
	"math"
	"testing"
)

func TestExpandedStageGeometryAndRoutes(t *testing.T) {
	s := testState(t)
	if world.Width != 4800 || world.Height != 1080 {
		t.Fatal("incorrect stage dimensions")
	}
	kinds := map[string]bool{}
	for _, n := range s.npcs {
		kinds[n.Kind] = true
		before := n.HP
		s.advance(n)
		if n.HP != before {
			t.Fatal("old kill plane damages expanded stage")
		}
	}
	if len(kinds) != 5 {
		t.Fatalf("expected five pirate types, got %v", kinds)
	}
	// Match the routing graph's actual jump constraints from the floor upward.
	reachable := make([]bool, len(world.Solids))
	for i, r := range world.Solids {
		if r.Y == 1024 && r.W == 4800 {
			reachable[i] = true
		}
	}
	for pass := 0; pass < len(reachable); pass++ {
		changed := false
		for i, r := range world.Solids {
			if reachable[i] {
				for j, next := range world.Solids {
					gap := math.Max(0, math.Max(next.X-(r.X+r.W), r.X-(next.X+next.W)))
					if !reachable[j] && next.Y >= r.Y-84 && gap <= 95 {
						reachable[j] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	for i, r := range world.Solids {
		if r.OneWay && !reachable[i] {
			t.Fatalf("unreachable platform: %+v", r)
		}
	}
	for _, p := range world.Players {
		a := Actor{Slot: 0, HP: 100, X: p.X, Y: p.Y, Stamina: 100}
		for i := 0; i < 90; i++ {
			s.advance(&a)
		}
		if a.HP != 100 || math.Abs(a.Y-p.Y) > 1 {
			t.Fatalf("unsafe player spawn: %+v", a)
		}
	}
}

func TestBombThrowAndReactions(t *testing.T) {
	s := testState(t)
	target := &Actor{Slot: 0, HP: 100, X: 360, Y: 1024}
	thrower := &Actor{Kind: "big-guy", Slot: -1, HP: 100, X: 192, Y: 1024, Face: 1, grounded: true, target: target}
	if !s.thinkSpecial(thrower) || thrower.special != "PickBomb" {
		t.Fatal("big guy failed to prepare bomb")
	}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		if thrower.special != "" {
			seen[thrower.special] = true
		}
		s.advanceSpecial(thrower)
	}
	for _, action := range []string{"PickBomb", "IdleBomb", "RunBomb", "ThrowBomb"} {
		if !seen[action] {
			t.Fatal("missing bomb action", action)
		}
	}
	if len(s.Bombs) != 1 {
		t.Fatal("throw animation did not create bomb")
	}
	b := s.Bombs[0]
	cucumber := &Actor{Kind: "cucumber", Slot: -1, HP: 100, X: b.X, Y: b.Y}
	if !s.thinkSpecial(cucumber) || cucumber.special != "BlowTheWick" || b.Anim != "BombOff" {
		t.Fatal("cucumber failed to extinguish bomb")
	}
	s.throwBomb(thrower)
	b = s.Bombs[1]
	captain := &Actor{Kind: "captain", Slot: -1, HP: 100, X: b.X - 20, Y: b.Y}
	if !s.thinkSpecial(captain) || !captain.fleeing || captain.input.Move != -1 {
		t.Fatal("captain failed to flee")
	}
	whale := &Actor{Kind: "whale", Slot: -1, HP: 100, X: b.X, Y: b.Y}
	if !s.thinkSpecial(whale) || whale.special != "SwallowBomb" || b.ticks != 1 {
		t.Fatal("whale failed to swallow bomb")
	}
	s.Bombs = nil
	s.throwBomb(thrower)
	b = s.Bombs[0]
	b.X = target.X
	b.Y = target.Y
	b.vx = 0
	b.vy = 0
	b.ticks = 1
	s.players = []*Actor{target}
	s.stepBombs()
	if target.HP != 65 || b.Anim != "Explosion" {
		t.Fatal("bomb failed to explode and damage player")
	}
	for i := 0; i < 23; i++ {
		s.stepBombs()
	}
	if len(s.Bombs) != 0 {
		t.Fatal("explosion not retired")
	}
}

func TestPirateAnimationStates(t *testing.T) {
	s := testState(t)
	a := spawnNPC(NPCSpawn{Point: Point{192, 1024}, Kind: "bald-pirate"}, 1)
	a.grounded = true
	a.input.Jump = true
	s.advance(&a)
	if a.Anim != "JumpAnticipation" {
		t.Fatal(a.Anim)
	}
	a.input.Jump = false
	for i := 0; i < 3; i++ {
		s.advance(&a)
	}
	if a.Anim != "Jump" || a.VY >= 0 {
		t.Fatal("anticipated jump did not launch")
	}
	s.damage(&a, nil, 20)
	s.advance(&a)
	if a.Anim != "Hit" {
		t.Fatal(a.Anim)
	}
	a.invulnerable = 0
	s.damage(&a, nil, 100)
	s.advance(&a)
	if a.Anim != "DeadHit" {
		t.Fatal(a.Anim)
	}
	for i := 0; i < 12; i++ {
		s.advance(&a)
	}
	if a.Anim != "DeadGround" {
		t.Fatal(a.Anim)
	}
}

func TestEnemyClimbsExpandedTiers(t *testing.T) {
	s := testState(t)
	target := &Actor{Slot: 0, HP: 100, X: 320, Y: 192}
	s.players = []*Actor{target}
	n := spawnNPC(NPCSpawn{Point: Point{192, 1024}, Kind: "bald-pirate"}, 1)
	minimum := n.Y
	for tick := 0; tick < 90*tickRate; tick++ {
		s.think(&n)
		s.advance(&n)
		minimum = math.Min(minimum, n.Y)
		if n.Y <= 200 {
			return
		}
	}
	t.Fatalf("enemy could not climb stage tiers; best height %.1f, final position %.1f, %.1f", minimum, n.X, n.Y)
}
