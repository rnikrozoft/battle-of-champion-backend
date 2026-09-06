package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
)

// Phase one deliberately admits only P1. Slot-aware simulation and scoring support P2.
const playerLimit = 1

func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	if err := loadArena(); err != nil {
		return err
	}
	if err := initializer.RegisterMatch("survival_arena", func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule) (runtime.Match, error) {
		return &ArenaMatch{}, nil
	}); err != nil {
		return err
	}
	return initializer.RegisterRpc("create_arena", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		uid, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
		if !ok || uid == "" {
			return "", runtime.NewError("Authentication required", 16)
		}
		// One live owned match per guest; relaunches cannot create unbounded idle matches.
		matches, err := nk.MatchList(ctx, 1, true, "", nil, nil, "+label.owner:"+uid)
		if err != nil {
			return "", err
		}
		if len(matches) > 0 {
			return fmt.Sprintf(`{"match_id":%q}`, matches[0].MatchId), nil
		}
		id, err := nk.MatchCreate(ctx, "survival_arena", map[string]interface{}{"owner": uid})
		if err != nil {
			return "", err
		}
		result, _ := json.Marshal(map[string]string{"match_id": id})
		return string(result), nil
	})
}

type ArenaMatch struct{}

func (*ArenaMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	owner, _ := params["owner"].(string)
	s := newState(owner)
	label, _ := json.Marshal(map[string]string{"owner": owner, "phase": "solo"})
	return s, tickRate, string(label)
}
func (*ArenaMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	s := state.(*State)
	if s.Ended {
		return s, false, "Match finished"
	}
	if playerLimit == 1 && presence.GetUserId() != s.owner {
		return s, false, "Phase one is Player 1 only"
	}
	for _, p := range s.players {
		if p.ID == presence.GetUserId() {
			return s, false, "Guest already connected"
		}
	}
	return s, len(s.players) < playerLimit, "Match full"
}
func (*ArenaMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*State)
	for _, presence := range presences {
		if len(s.players) >= playerLimit {
			continue
		}
		slot := 0
		for _, p := range s.players {
			if p.Slot == 0 {
				slot = 1
			}
		}
		spawn := world.Players[slot]
		p := &Actor{ID: presence.GetUserId(), Slot: slot, X: spawn.X, Y: spawn.Y, HP: 100, Face: 1, Anim: "Idle", session: presence.GetSessionId()}
		if slot == 1 {
			p.Face = -1
		}
		s.players = append(s.players, p)
	}
	if !s.started && len(s.players) > 0 {
		s.started = true
		s.startTick = tick
		s.nextSpawn = tick + tickRate*4
	}
	s.broadcast(dispatcher, tick, logger)
	return s
}
func (*ArenaMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*State)
	for _, presence := range presences {
		for i := len(s.players) - 1; i >= 0; i-- {
			if s.players[i].session == presence.GetSessionId() {
				s.players = append(s.players[:i], s.players[i+1:]...)
			}
		}
	}
	if len(s.players) == 0 {
		return nil
	}
	return s
}
func (*ArenaMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	s := state.(*State)
	if !s.started {
		if tick > tickRate*30 {
			return nil
		}
		return s
	}
	if s.Ended {
		if tick-s.endTick > tickRate*15 {
			return nil
		}
		return s
	}
	for _, message := range messages {
		if message.GetOpCode() != 1 || len(message.GetData()) > 256 {
			continue
		}
		var in Input
		if json.Unmarshal(message.GetData(), &in) != nil || in.Move < -1 || in.Move > 1 {
			continue
		}
		for _, p := range s.players {
			if p.ID != message.GetUserId() || p.session != message.GetSessionId() || in.Seq <= p.input.Seq {
				continue
			}
			// Preserve edge-triggered buttons if several messages arrive in one simulation tick.
			in.Jump = in.Jump || p.input.Jump
			in.Dash = in.Dash || p.input.Dash
			in.Attack = in.Attack || p.input.Attack
			p.input = in
			p.lastInput = tick
		}
	}
	s.Remaining = max(0, 180-float64(tick-s.startTick)/tickRate)
	if s.Remaining <= 0 {
		s.finish(tick)
		s.broadcast(dispatcher, tick, logger)
		return s
	}
	s.step(tick)
	if tick%2 == 0 {
		s.broadcast(dispatcher, tick, logger)
	}
	return s
}
func (*ArenaMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, graceSeconds int) interface{} {
	s := state.(*State)
	s.Ended = true
	s.Result = "Server shutting down"
	s.broadcast(dispatcher, tick, logger)
	return s
}
func (*ArenaMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	return state, ""
}
