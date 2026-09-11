package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (h TankHero) arenaLocked(now int64) bool { return h.ArenaMatch != "" && h.ArenaUntil > now }

// Called under the same tank row lock used by theft. A selection is checked and
// reserved atomically; client metadata can never lock another player's hero.
func reserveArenaHero(t *TankState, heroID int64, matchID string, now int64) (TankHero, error) {
	for _, h := range t.Heroes {
		if h.arenaLocked(now) {
			return TankHero{}, fmt.Errorf("A hero is already deployed in Arena")
		}
	}
	if heroID == 0 && len(t.Heroes) > 0 {
		heroID = t.Heroes[0].ID
	}
	for i := range t.Heroes {
		h := &t.Heroes[i]
		if h.ID != heroID {
			continue
		}
		h.ArenaMatch = matchID
		// Bounded recovery for process crashes or a join attempt that never completes.
		h.ArenaUntil = now + 210
		return *h, nil
	}
	return TankHero{}, fmt.Errorf("Selected hero is no longer owned")
}
func releaseArenaHeroes(t *TankState, matchID string) bool {
	changed := false
	for i := range t.Heroes {
		if t.Heroes[i].ArenaMatch == matchID {
			t.Heroes[i].ArenaMatch = ""
			t.Heroes[i].ArenaUntil = 0
			changed = true
		}
	}
	return changed
}
func editArenaRoster(ctx context.Context, db *sql.DB, uid string, edit func(*TankState) (bool, error)) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	initial, _ := json.Marshal(initialTank(uid))
	if _, err = tx.ExecContext(ctx, `INSERT INTO pirate_tanks(user_id,state) VALUES ($1::uuid,$2::jsonb) ON CONFLICT DO NOTHING`, uid, string(initial)); err != nil {
		return err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT state FROM pirate_tanks WHERE user_id=$1::uuid FOR UPDATE`, uid).Scan(&raw); err != nil {
		return err
	}
	var t TankState
	if err = json.Unmarshal(raw, &t); err != nil {
		return err
	}
	changed, err := edit(&t)
	if err != nil {
		return err
	}
	if changed {
		t.Version++
		raw, _ = json.Marshal(t)
		if _, err = tx.ExecContext(ctx, `UPDATE pirate_tanks SET state=$2::jsonb WHERE user_id=$1::uuid`, uid, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type arenaReservation struct {
	UserID, Kind string
	Joined       bool
}

func (s *State) releaseRoster(ctx context.Context, db *sql.DB) error {
	for session, r := range s.roster {
		if err := editArenaRoster(ctx, db, r.UserID, func(t *TankState) (bool, error) { return releaseArenaHeroes(t, s.matchID), nil }); err != nil {
			return err
		}
		delete(s.roster, session)
	}
	return nil
}
func acquireArenaRoster(ctx context.Context, db *sql.DB, uid, matchID string, heroID int64) (TankHero, error) {
	var h TankHero
	err := editArenaRoster(ctx, db, uid, func(t *TankState) (bool, error) {
		var err error
		h, err = reserveArenaHero(t, heroID, matchID, time.Now().Unix())
		return err == nil, err
	})
	return h, err
}
