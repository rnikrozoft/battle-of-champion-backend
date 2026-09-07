package main

import (
	"fmt"
	"math"
)

func isPirate(a *Actor) bool { return a.Kind != "" && a.Kind != "pig" && a.Kind != "bomb" }
func spawnNPC(p NPCSpawn, serial int) Actor {
	kind := p.Kind
	if kind == "" {
		kind = "pig"
	}
	return Actor{ID: fmt.Sprintf("npc-%d", serial), Kind: kind, Slot: -1, X: p.X, Y: p.Y, HP: 100, Face: 1, Anim: "Idle", aiCadence: 8 + serial%4}
}
func npcSpeed(kind string) float64 {
	switch kind {
	case "bald-pirate":
		return 72
	case "captain":
		return 84
	case "big-guy":
		return 48
	case "whale":
		return 54
	case "cucumber":
		return 64
	default:
		return 58
	}
}

type Bomb struct {
	ID     int     `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Anim   string  `json:"anim"`
	vx, vy float64
	ticks  int
	owner  *Actor
}

func (s *State) throwBomb(a *Actor) {
	if len(s.Bombs) >= 24 {
		return
	}
	s.bombSerial++
	s.Bombs = append(s.Bombs, &Bomb{ID: s.bombSerial, X: a.X + float64(a.Face)*20, Y: a.Y - 24, Anim: "BombOn", vx: float64(a.Face) * 120, vy: -140, ticks: 75, owner: a})
}

func (s *State) stepBombs() {
	for i := len(s.Bombs) - 1; i >= 0; i-- {
		b := s.Bombs[i]
		b.ticks--
		if b.Anim == "BombOn" {
			body := Actor{Slot: -1, Kind: "bomb", X: b.X, Y: b.Y, VX: b.vx, VY: math.Min(320, b.vy+580*dt)}
			moveActor(&body)
			b.X, b.Y, b.vx, b.vy = body.X, body.Y, body.VX, body.VY
			if body.grounded {
				b.vx *= .85
			}
			if b.ticks <= 0 {
				b.Anim = "Explosion"
				b.ticks = 23
				for _, group := range [][]*Actor{s.players, s.npcs} {
					for _, a := range group {
						if math.Hypot(a.X-b.X, a.Y-12-b.Y) < 64 {
							s.damage(a, b.owner, 35)
						}
					}
				}
			}
		} else if b.ticks <= 0 {
			s.Bombs = append(s.Bombs[:i], s.Bombs[i+1:]...)
		}
	}
}

func (s *State) nearbyBomb(a *Actor, radius float64) *Bomb {
	for _, b := range s.Bombs {
		if b.Anim == "BombOn" && math.Hypot(b.X-a.X, b.Y-a.Y) < radius {
			return b
		}
	}
	return nil
}

func (s *State) thinkSpecial(a *Actor) bool {
	a.fleeing = false
	if a.specialTicks > 0 {
		if a.special == "RunBomb" && a.target != nil {
			if a.target.X < a.X {
				a.input.Move = -1
			} else {
				a.input.Move = 1
			}
		}
		return true
	}
	if a.specialCooldown > 0 {
		return false
	}
	if a.Kind == "cucumber" || a.Kind == "whale" {
		if b := s.nearbyBomb(a, 40); b != nil {
			// Reserve/disable the bomb immediately; the special animation shows the reaction.
			b.Anim = "BombOff"
			b.ticks = 30
			a.special = "BlowTheWick"
			a.specialTicks = 28
			if a.Kind == "whale" {
				a.special = "SwallowBomb"
				a.specialTicks = 25
				b.ticks = 1
			}
			a.specialCooldown = 90
			return true
		}
	}
	if a.Kind == "captain" {
		if b := s.nearbyBomb(a, 100); b != nil {
			a.fleeing = true
			if b.X < a.X {
				a.input.Move = 1
			} else {
				a.input.Move = -1
			}
			return true
		}
	}
	if a.Kind == "big-guy" && a.target != nil && a.target.HP > 0 && a.grounded {
		distance := math.Abs(a.target.X - a.X)
		if distance > 48 && distance < 280 && math.Abs(a.target.Y-a.Y) < 64 {
			a.special = "PickBomb"
			a.specialTicks = 20
			a.specialCooldown = 180
			return true
		}
	}
	return false
}

func (s *State) advanceSpecial(a *Actor) {
	if a.specialTicks <= 0 || a.HP <= 0 {
		return
	}
	a.Anim = a.special
	a.specialTicks--
	if a.specialTicks > 0 {
		return
	}
	switch a.special {
	case "PickBomb":
		a.special = "IdleBomb"
		a.specialTicks = 6
	case "IdleBomb":
		a.special = "RunBomb"
		a.specialTicks = 40
	case "RunBomb":
		a.special = "ThrowBomb"
		a.specialTicks = 28
	case "ThrowBomb":
		s.throwBomb(a)
		a.special = ""
	default:
		a.special = ""
	}
}
