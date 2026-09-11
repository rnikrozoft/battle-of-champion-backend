package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"strconv"
)

const playerLimit = 10

func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS pirate_tanks (user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, state JSONB NOT NULL)`); err != nil {
		return err
	}
	if err := initializer.RegisterRpc("tank", tankRPC); err != nil {
		return err
	}
	if err := loadArena(); err != nil {
		return err
	}
	if err := initializer.RegisterMatch("survival_arena", func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule) (runtime.Match, error) {
		return &ArenaMatch{}, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterRpc("create_arena", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		uid, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
		if !ok || uid == "" {
			return "", runtime.NewError("Authentication required", 16)
		}
		return resolveRoom(ctx, nk, uid, payload)
	}); err != nil {
		return err
	}
	return initializer.RegisterRpc("list_tank_players", listTankPlayers)
}

type ArenaMatch struct {
	reserve func(context.Context, *sql.DB, string, string, int64) (TankHero, error)
}

func (*ArenaMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	owner, _ := params["owner"].(string)
	s := newState(owner)
	s.matchID, _ = ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
	s.roster = make(map[string]arenaReservation)
	s.RoomCode, _ = params["room_code"].(string)
	label, _ := json.Marshal(map[string]string{"owner": owner, "room_code": s.RoomCode})
	return s, tickRate, string(label)
}
func (m *ArenaMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	s := state.(*State)
	if s.Ended {
		return s, false, "Match finished"
	}
	for _, p := range s.players {
		if p.ID == presence.GetUserId() {
			return s, false, "Guest already connected"
		}
	}
	if len(s.players) >= playerLimit {
		return s, false, "Match full"
	}
	heroID, err := strconv.ParseInt(metadata["hero_id"], 10, 64)
	if metadata["hero_id"] != "" && (err != nil || heroID < 0) {
		return s, false, "Invalid hero selection"
	}
	reserve := m.reserve
	if reserve == nil {
		reserve = acquireArenaRoster
	}
	hero, err := reserve(ctx, db, presence.GetUserId(), s.matchID, heroID)
	if err != nil {
		return s, false, err.Error()
	}
	s.roster[presence.GetSessionId()] = arenaReservation{UserID: presence.GetUserId(), Kind: hero.Kind}
	return s, true, ""
}
func (*ArenaMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*State)
	for _, presence := range presences {
		duplicate := false
		for _, p := range s.players {
			if p.ID == presence.GetUserId() {
				duplicate = true
				break
			}
		}
		if s.Ended || len(s.players) >= playerLimit || duplicate {
			if err := dispatcher.MatchKick([]runtime.Presence{presence}); err != nil {
				logger.Warn("reject excess room presence: %v", err)
			}
			continue
		}
		reservation, reserved := s.roster[presence.GetSessionId()]
		if !reserved {
			dispatcher.MatchKick([]runtime.Presence{presence})
			continue
		}
		reservation.Joined = true
		s.roster[presence.GetSessionId()] = reservation
		slot := freeSlot(s.players)
		spawn := playerSpawn(slot)
		p := &Actor{Kind: reservation.Kind, ID: presence.GetUserId(), Slot: slot, X: spawn.X, Y: spawn.Y, HP: 100, Stamina: 100, Face: 1, Anim: "Idle", session: presence.GetSessionId()}
		if slot%2 == 1 {
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
				for _, n := range s.npcs {
					if n.target == s.players[i] {
						n.target = nil
					}
				}
				s.players = append(s.players[:i], s.players[i+1:]...)
			}
		}
	}
	if len(s.players) == 0 {
		if err := s.releaseRoster(ctx, db); err != nil {
			logger.Error("Release Arena roster: %v", err)
			return s
		}
		if !s.Ended {
			releaseRoom(s.RoomCode)
		}
		return nil
	}
	return s
}
func (*ArenaMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	s := state.(*State)
	if !s.started {
		if tick > tickRate*30 {
			if err := s.releaseRoster(ctx, db); err != nil {
				logger.Error("Release unjoined roster: %v", err)
				return s
			}
			releaseRoom(s.RoomCode)
			return nil
		}
		return s
	}
	if s.Ended {
		if err := s.releaseRoster(ctx, db); err != nil {
			logger.Error("Retry Arena roster release: %v", err)
		}
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
		if err := s.releaseRoster(ctx, db); err != nil {
			logger.Error("Release Arena roster: %v", err)
		}
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
	if !s.Ended {
		releaseRoom(s.RoomCode)
	}
	if err := s.releaseRoster(ctx, db); err != nil {
		logger.Error("Release shutdown roster: %v", err)
	}
	s.Ended = true
	s.Result = "Server shutting down"
	s.broadcast(dispatcher, tick, logger)
	return s
}
func (*ArenaMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	return state, ""
}
