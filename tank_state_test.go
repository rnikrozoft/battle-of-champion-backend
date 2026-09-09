package main

import "testing"

func TestTankCompetingActions(t *testing.T) {
	for _, action := range []string{"sell", "breed", "upgrade"} {
		for _, stealFirst := range []bool{true, false} {
			t.Run(action+map[bool]string{true: "/theft-first", false: "/owner-first"}[stealFirst], func(t *testing.T) {
				owner, thief := initialTank("owner"), initialTank("thief")
				owner.Coins = 100
				ownReq := TankRequest{Action: action, HeroID: 1, PartnerID: 2, Version: 1, SelfVersion: 1}
				theft := TankRequest{Action: "steal", HeroID: 1, Version: 1, SelfVersion: 1}
				if stealFirst {
					if err := applyTankAction(&owner, &thief, theft, 100); err != nil {
						t.Fatal(err)
					}
					owner.Version++
					thief.Version++
					if err := applyTankAction(&owner, &owner, ownReq, 100); err == nil {
						t.Fatal("stale owner action succeeded")
					}
					if owner.Coins != 100 || len(owner.Heroes) != 4 || len(thief.Heroes) != 6 {
						t.Fatal("theft changed wrong state")
					}
				} else {
					if err := applyTankAction(&owner, &owner, ownReq, 100); err != nil {
						t.Fatal(err)
					}
					owner.Version++
					if err := applyTankAction(&owner, &thief, theft, 100); err == nil {
						t.Fatal("stale theft succeeded")
					}
					if len(thief.Heroes) != 5 {
						t.Fatal("loser acquired a hero")
					}
				}
			})
		}
	}
}

func TestTankPartnerAndOwnershipChecks(t *testing.T) {
	owner, thief := initialTank("owner"), initialTank("thief")
	owner.Coins = 100
	req := TankRequest{Action: "breed", HeroID: 1, PartnerID: 2, Version: 1, SelfVersion: 1}
	if err := applyTankAction(&owner, &thief, req, 100); err == nil {
		t.Fatal("foreign breeding accepted")
	}
	owner.Heroes = append(owner.Heroes[:1], owner.Heroes[2:]...)
	if err := applyTankAction(&owner, &owner, req, 100); err == nil {
		t.Fatal("missing partner accepted")
	}
	if owner.Coins != 100 || len(owner.Heroes) != 4 {
		t.Fatal("failed request mutated account")
	}
}

func TestCrustyAttackAnimations(t *testing.T) {
	s := testState(t)
	for _, kind := range []string{"crabby", "fierce-tooth", "pink-star"} {
		a := spawnNPC(NPCSpawn{Kind: kind, Point: Point{X: 192, Y: 1024}}, 1)
		a.grounded = true
		a.input.Attack = true
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			s.advance(&a)
			a.input.Attack = false
			seen[a.Anim] = true
		}
		if !seen["Anticipation"] || !seen["Attack"] || !seen["Idle"] {
			t.Fatalf("%s incomplete attack: %v", kind, seen)
		}
		s.damage(&a, nil, 100)
		seen = map[string]bool{}
		for i := 0; i < 30; i++ {
			s.advance(&a)
			seen[a.Anim] = true
		}
		if !seen["DeadHit"] || !seen["DeadGround"] {
			t.Fatalf("%s incomplete death", kind)
		}
	}
}
