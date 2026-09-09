package main

import "testing"

func TestMovementEventsSurviveAttackAnimation(t *testing.T) {
	s := testState(t)
	a := Actor{Slot: 0, HP: 100, X: 192, Y: 1024, grounded: true, Face: 1}
	a.input = Input{Jump: true, Attack: true, Move: 1}
	s.advance(&a)
	if a.JumpEvent != 1 || a.Grounded || a.Anim != "Attack" {
		t.Fatalf("jump during attack missing: %+v", a)
	}
	a.input = Input{}
	for i := 0; i < 120 && !a.Grounded; i++ {
		s.advance(&a)
	}
	if !a.Grounded || a.LandEvent != 1 {
		t.Fatalf("landing event missing: %+v", a)
	}
	for i := 0; i < 15; i++ {
		s.advance(&a)
	}
	if a.LandEvent != 1 || a.JumpEvent != 1 {
		t.Fatal("stationary updates repeat effects")
	}
}
