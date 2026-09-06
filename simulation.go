package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"math"
)

const tickRate = 30
const dt = 1.0 / tickRate
const maxNPC = 24

//go:embed arena.json
var arenaData []byte
var world World

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Solid struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	OneWay bool    `json:"one_way"`
}
type World struct {
	PlayerBody Point   `json:"player_body"`
	NPCBody    Point   `json:"npc_body"`
	Solids     []Solid `json:"solids"`
	Players    []Point `json:"players"`
	NPCs       []Point `json:"npcs"`
}

func loadArena() error {
	if err := json.Unmarshal(arenaData, &world); err != nil {
		return err
	}
	if world.PlayerBody.X <= 0 {
		world.PlayerBody = Point{18, 22}
	}
	if world.NPCBody.X <= 0 {
		world.NPCBody = Point{18, 17}
	}
	if len(world.Players) != 2 || len(world.NPCs) == 0 || len(world.Solids) == 0 {
		return fmt.Errorf("arena needs collision, two player markers and NPC markers")
	}
	return nil
}

type Input struct {
	Seq    int64 `json:"seq"`
	Move   int   `json:"move"`
	Jump   bool  `json:"jump"`
	Dash   bool  `json:"dash"`
	Attack bool  `json:"attack"`
}
type Actor struct {
	ID             string  `json:"id"`
	Slot           int     `json:"slot"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	VX             float64 `json:"vx"`
	VY             float64 `json:"vy"`
	HP             int     `json:"hp"`
	Deaths         int     `json:"deaths"`
	Kills          int     `json:"kills"`
	Face           int     `json:"face"`
	Anim           string  `json:"anim"`
	Hit            int     `json:"hit"`
	Attack         int     `json:"attack"`
	input          Input
	session        string
	lastInput      int64
	grounded       bool
	jumps          int
	dashTicks      int
	dashCooldown   int
	attackTicks    int
	attackCooldown int
	invulnerable   int
	deadTicks      int
	landTicks      int
	aiTick         int
	aiCadence      int
	goalX          float64
	goalY          float64
	target         *Actor
}
type State struct {
	Tick      int64    `json:"tick"`
	Remaining float64  `json:"remaining"`
	Ended     bool     `json:"ended"`
	Result    string   `json:"result"`
	Actors    []*Actor `json:"actors"`
	owner     string
	players   []*Actor
	npcs      []*Actor
	pool      []*Actor
	started   bool
	startTick int64
	endTick   int64
	nextSpawn int64
	serial    int
	navPrev   []int
	navQueue  []int
}

func newState(owner string) *State {
	s := &State{owner: owner, Remaining: 180, players: make([]*Actor, 0, 2), npcs: make([]*Actor, 0, maxNPC), pool: make([]*Actor, 0, maxNPC), Actors: make([]*Actor, 0, maxNPC+2)}
	s.navPrev = make([]int, len(world.Solids))
	s.navQueue = make([]int, len(world.Solids))
	for i := 0; i < maxNPC; i++ {
		s.pool = append(s.pool, &Actor{})
	}
	for _, point := range world.NPCs {
		if len(s.pool) == 0 {
			break
		}
		n := s.pool[len(s.pool)-1]
		s.pool = s.pool[:len(s.pool)-1]
		s.serial++
		*n = Actor{ID: fmt.Sprintf("npc-%d", s.serial), Slot: -1, X: point.X, Y: point.Y, HP: 100, Face: 1, Anim: "Idle", aiCadence: 8 + s.serial%4}
		s.npcs = append(s.npcs, n)
	}
	return s
}
func (s *State) step(tick int64) {
	if tick >= s.nextSpawn && len(s.pool) > 0 {
		point := world.NPCs[s.serial%len(world.NPCs)]
		n := s.pool[len(s.pool)-1]
		s.pool = s.pool[:len(s.pool)-1]
		s.serial++
		*n = Actor{ID: fmt.Sprintf("npc-%d", s.serial), Slot: -1, X: point.X, Y: point.Y, HP: 100, Face: 1, Anim: "Idle", aiCadence: 8 + s.serial%4}
		s.npcs = append(s.npcs, n)
		s.nextSpawn = tick + tickRate*4
	}
	for _, p := range s.players {
		if tick-p.lastInput > tickRate/2 {
			p.input.Move = 0
			p.input.Jump = false
			p.input.Dash = false
			p.input.Attack = false
		}
		s.advance(p)
		p.input.Jump = false
		p.input.Dash = false
		p.input.Attack = false
	}
	for i := len(s.npcs) - 1; i >= 0; i-- {
		n := s.npcs[i]
		if n.HP <= 0 && n.deadTicks <= 1 {
			s.pool = append(s.pool, n)
			s.npcs = append(s.npcs[:i], s.npcs[i+1:]...)
			continue
		}
		s.think(n)
		s.advance(n)
	}
}
func (s *State) advance(a *Actor) {
	if a.HP <= 0 {
		a.Anim = "Dead"
		a.deadTicks--
		if a.deadTicks <= 0 && a.Slot >= 0 {
			p := world.Players[a.Slot]
			a.X = p.X
			a.Y = p.Y
			a.HP = 100
			a.VX = 0
			a.VY = 0
			a.jumps = 0
			a.invulnerable = 30
			a.attackTicks = 0
			a.dashTicks = 0
		}
		return
	}
	if a.invulnerable > 0 {
		a.invulnerable--
	}
	if a.attackCooldown > 0 {
		a.attackCooldown--
	}
	if a.dashCooldown > 0 {
		a.dashCooldown--
	}
	if a.landTicks > 0 {
		a.landTicks--
	}
	if a.input.Move != 0 {
		a.Face = a.input.Move
	}
	if a.input.Jump && a.jumps < 2 {
		a.VY = -230
		a.jumps++
		a.grounded = false
	}
	if a.input.Dash && a.Slot >= 0 && a.dashCooldown == 0 {
		a.dashTicks = 5
		a.dashCooldown = 27
	}
	if a.input.Attack && a.attackCooldown == 0 {
		a.attackTicks = 9
		a.attackCooldown = 16
		a.Attack++
	}
	speed := 100.0
	if a.Slot < 0 {
		speed = 58
	}
	a.VX = float64(a.input.Move) * speed
	if a.dashTicks > 0 {
		a.VX = float64(a.Face) * 290
		a.VY = 0
		a.dashTicks--
	} else {
		a.VY = math.Min(320, a.VY+580*dt)
	}
	wasGrounded := a.grounded
	moveActor(a)
	if a.grounded {
		a.jumps = 0
		if !wasGrounded {
			a.landTicks = 3
		}
	} else if a.jumps == 0 {
		a.jumps = 1
	}
	if a.attackTicks > 0 {
		a.attackTicks--
		if a.attackTicks == 5 {
			s.strike(a)
		}
	}
	switch {
	case a.invulnerable > 0 && a.invulnerable < 9:
		a.Anim = "Hit"
	case a.attackTicks > 0:
		a.Anim = "Attack"
	case !a.grounded && a.VY < 0:
		a.Anim = "Jump"
	case !a.grounded:
		a.Anim = "Fall"
	case a.landTicks > 0:
		a.Anim = "Ground"
	case math.Abs(a.VX) > 1:
		a.Anim = "Run"
	default:
		a.Anim = "Idle"
	}
	if a.Y > 240 {
		s.damage(a, nil, 100)
	}
}
func size(a *Actor) (float64, float64) {
	if a.Slot < 0 {
		return world.NPCBody.X / 2, world.NPCBody.Y
	}
	return world.PlayerBody.X / 2, world.PlayerBody.Y
}
func moveActor(a *Actor) {
	half, height := size(a)
	// Substeps keep dash travel below the thinnest collider; same rectangles are in Godot.
	steps := int(math.Ceil(math.Max(math.Abs(a.VX), math.Abs(a.VY)) * dt / 3))
	if steps < 1 {
		steps = 1
	}
	step := dt / float64(steps)
	a.grounded = false
	for i := 0; i < steps; i++ {
		a.X += a.VX * step
		for _, r := range world.Solids {
			if r.OneWay || a.X+half <= r.X || a.X-half >= r.X+r.W || a.Y <= r.Y || a.Y-height >= r.Y+r.H {
				continue
			}
			if a.VX > 0 {
				a.X = r.X - half
			} else if a.VX < 0 {
				a.X = r.X + r.W + half
			}
			a.VX = 0
		}
		oldY := a.Y
		a.Y += a.VY * step
		for _, r := range world.Solids {
			if a.X+half <= r.X || a.X-half >= r.X+r.W || a.Y <= r.Y || a.Y-height >= r.Y+r.H {
				continue
			}
			if r.OneWay && (a.VY < 0 || oldY > r.Y+.1) {
				continue
			}
			if a.VY >= 0 {
				a.Y = r.Y
				a.grounded = true
			} else {
				a.Y = r.Y + r.H + height
			}
			a.VY = 0
		}
	}
}
func (s *State) strike(a *Actor) {
	if a.Slot >= 0 {
		for _, n := range s.npcs {
			if canHit(a, n) {
				s.damage(n, a, 25)
			}
		}
	}
	for _, p := range s.players {
		if p != a && canHit(a, p) {
			s.damage(p, a, 20)
		}
	}
}
func canHit(a, b *Actor) bool {
	if b.HP <= 0 {
		return false
	}
	dx := (b.X - a.X) * float64(a.Face)
	if dx < -4 || dx > 34 || math.Abs((a.Y-10)-(b.Y-10)) > 20 {
		return false
	}
	// Solid walls occlude melee attacks; decorative crates do not.
	x := (a.X + b.X) / 2
	y := (a.Y+b.Y)/2 - 9
	for _, r := range world.Solids {
		if !r.OneWay && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return false
		}
	}
	return true
}
func (s *State) damage(victim, attacker *Actor, amount int) {
	if victim.HP <= 0 || victim.invulnerable > 0 {
		return
	}
	victim.HP = max(0, victim.HP-amount)
	victim.Hit++
	victim.invulnerable = 8
	// Damage never changes velocity: deliberately no knockback.
	if victim.HP == 0 {
		victim.deadTicks = 30
		victim.attackTicks = 0
		victim.Anim = "Dead"
		if victim.Slot >= 0 {
			victim.Deaths++
		}
		if attacker != nil && attacker.Slot >= 0 && victim.Slot < 0 {
			attacker.Kills++
		}
	}
}
func (s *State) think(n *Actor) {
	n.input = Input{}
	if n.HP <= 0 {
		return
	}
	n.aiTick--
	if n.aiTick <= 0 || n.target == nil || n.target.HP <= 0 {
		n.aiTick = n.aiCadence
		n.target = nil
		best := math.MaxFloat64
		for _, p := range s.players {
			d := math.Abs(p.X-n.X) + math.Abs(p.Y-n.Y)
			if p.HP > 0 && d < best {
				best = d
				n.target = p
			}
		}
		if n.target == nil {
			return
		}
		n.goalX, n.goalY = s.route(n, n.target)
	}
	if n.target == nil {
		return
	}
	dx := n.goalX - n.X
	if math.Abs(dx) > 3 {
		if dx < 0 {
			n.input.Move = -1
		} else {
			n.input.Move = 1
		}
	}
	if math.Abs(n.target.X-n.X) < 30 && math.Abs(n.target.Y-n.Y) < 20 {
		n.input.Move = 0
		if n.target.X < n.X {
			n.Face = -1
		} else {
			n.Face = 1
		}
		n.input.Attack = true
	}
	// Jump toward a higher surface, over obstacles, then spend second jump near apex.
	if n.goalY < n.Y-6 || (n.grounded && math.Abs(n.VX) < 1 && n.input.Move != 0) {
		n.input.Jump = n.grounded || (n.jumps == 1 && n.VY > -35)
	}
}
func support(a *Actor) int {
	best := -1
	distance := math.MaxFloat64
	for i, r := range world.Solids {
		if r.Y < a.Y-2 || a.X < r.X-8 || a.X > r.X+r.W+8 {
			continue
		}
		if r.Y-a.Y < distance {
			best = i
			distance = r.Y - a.Y
		}
	}
	return best
}
func (s *State) route(a, b *Actor) (float64, float64) {
	start, goal := support(a), support(b)
	if start < 0 || goal < 0 || start == goal {
		return b.X, b.Y
	}
	// Bounded BFS over authored platform tops. Recomputed only ~4 times/second per NPC.
	prev := s.navPrev
	queue := s.navQueue[:1]
	queue[0] = start
	for i := range prev {
		prev[i] = -2
	}
	prev[start] = -1
	for head := 0; head < len(queue); head++ {
		at := queue[head]
		r := world.Solids[at]
		for i, t := range world.Solids {
			gap := math.Max(0, math.Max(t.X-(r.X+r.W), r.X-(t.X+t.W)))
			if prev[i] != -2 || t.Y < r.Y-84 || gap > 95 {
				continue
			}
			prev[i] = at
			queue = append(queue, i)
		}
	}
	if prev[goal] == -2 {
		return b.X, b.Y
	}
	next := goal
	for prev[next] >= 0 && prev[next] != start {
		next = prev[next]
	}
	target := world.Solids[next]
	return math.Max(target.X+9, math.Min(a.X, target.X+target.W-9)), target.Y
}
func (s *State) finish(tick int64) {
	s.Ended = true
	s.endTick = tick
	if len(s.players) == 1 {
		p := s.players[0]
		s.Result = fmt.Sprintf("Time! %d deaths · %d NPC kills", p.Deaths, p.Kills)
		return
	}
	if len(s.players) < 2 {
		s.Result = "Match ended"
		return
	}
	a, b := s.players[0], s.players[1]
	winner := ""
	if a.Deaths < b.Deaths {
		winner = a.ID
	} else if b.Deaths < a.Deaths {
		winner = b.ID
	} else if a.Deaths == 0 {
		if a.Kills > b.Kills {
			winner = a.ID
		} else if b.Kills > a.Kills {
			winner = b.ID
		}
	}
	if winner == "" {
		s.Result = "Draw"
	} else {
		s.Result = winner + " wins"
	}
}
func (s *State) broadcast(dispatcher runtime.MatchDispatcher, tick int64, logger runtime.Logger) {
	s.Tick = tick
	s.Actors = s.Actors[:0]
	s.Actors = append(s.Actors, s.players...)
	s.Actors = append(s.Actors, s.npcs...)
	data, err := json.Marshal(s)
	if err != nil {
		logger.Error("snapshot: %v", err)
		return
	}
	if err = dispatcher.BroadcastMessage(2, data, nil, nil, true); err != nil {
		logger.Warn("broadcast: %v", err)
	}
}
