package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

type TankHero struct {
	ID       int64   `json:"id"`
	Kind     string  `json:"kind"`
	Level    int     `json:"level"`
	Hunger   float64 `json:"hunger"`
	Tank     bool    `json:"tank"`
	RewardAt int64   `json:"reward_at"`
}
type TankState struct {
	Owner   string     `json:"owner"`
	Heroes  []TankHero `json:"heroes"`
	Coins   int        `json:"coins"`
	Version int64      `json:"version"`
	NextID  int64      `json:"next_id"`
}
type TankRequest struct {
	Kind        string `json:"kind"`
	Action      string `json:"action"`
	Owner       string `json:"owner"`
	HeroID      int64  `json:"hero_id"`
	PartnerID   int64  `json:"partner_id"`
	Version     int64  `json:"version"`
	SelfVersion int64  `json:"self_version"`
}

func initialTank(owner string) TankState {
	t := TankState{Owner: owner, Version: 1, NextID: 6, Heroes: make([]TankHero, 0, 5)}
	for i, k := range []string{"bald-pirate", "cucumber", "big-guy", "captain", "whale"} {
		t.Heroes = append(t.Heroes, TankHero{ID: int64(i + 1), Kind: k, Level: 1, Hunger: 100, Tank: true, RewardAt: time.Now().Unix() + 5})
	}
	return t
}

// Both accounts are locked in a stable order in the database, including across
// Nakama nodes. Versions reject competing requests based on an obsolete screen.
func tankRPC(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	uid, _ := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if uid == "" {
		return "", runtime.NewError("Authentication required", 16)
	}
	var req TankRequest
	if len(payload) > 2048 || json.Unmarshal([]byte(payload), &req) != nil {
		return "", runtime.NewError("Invalid tank request", 3)
	}
	if req.Owner == "" {
		req.Owner = uid
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	ids := []string{uid}
	if req.Owner != uid {
		ids = append(ids, req.Owner)
	}
	sort.Strings(ids)
	states := map[string]*TankState{}
	for _, id := range ids {
		initial, _ := json.Marshal(initialTank(id))
		// A foreign key prevents creating tanks for nonexistent users.
		if _, err = tx.ExecContext(ctx, `INSERT INTO pirate_tanks(user_id,state) VALUES ($1::uuid,$2::jsonb) ON CONFLICT DO NOTHING`, id, string(initial)); err != nil {
			return "", runtime.NewError("Unknown tank owner", 3)
		}
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT state FROM pirate_tanks WHERE user_id=$1::uuid FOR UPDATE`, id).Scan(&raw); err != nil {
			return "", err
		}
		var t TankState
		if err = json.Unmarshal(raw, &t); err != nil {
			return "", err
		}
		states[id] = &t
	}
	owner, self := states[req.Owner], states[uid]
	changed := req.Action != "" && req.Action != "view"
	if changed {
		if err = applyTankAction(owner, self, req, time.Now().Unix()); err != nil {
			return "", runtime.NewError(err.Error(), 9)
		}
		for _, id := range ids {
			t := states[id]
			t.Version++
			raw, _ := json.Marshal(t)
			if _, err = tx.ExecContext(ctx, `UPDATE pirate_tanks SET state=$2::jsonb WHERE user_id=$1::uuid`, id, string(raw)); err != nil {
				return "", err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	if changed {
		for _, id := range ids {
			if err = nk.NotificationSend(ctx, id, "tank_changed", map[string]interface{}{"owner": id, "version": states[id].Version}, 101, "", false); err != nil {
				logger.Warn("Tank notification: %v", err)
			}
		}
	}
	raw, err := json.Marshal(map[string]interface{}{"tank": owner, "self": self, "shop": shopCatalog})
	return string(raw), err
}

func applyTankAction(owner, self *TankState, req TankRequest, now int64) error {
	if req.Version != owner.Version || req.SelfVersion != self.Version {
		return fmt.Errorf("Tank changed. Refresh and try again")
	}
	if req.Action != "steal" && owner != self {
		return fmt.Errorf("You do not own this hero")
	}
	if req.Action == "buy" {
		return buyHero(self, req.Kind, now)
	}
	if req.Action == "feed" {
		for i := range self.Heroes {
			if self.Heroes[i].Tank {
				self.Heroes[i].Hunger = 100
			}
		}
		return nil
	}
	idx := -1
	for i, h := range owner.Heroes {
		if h.ID == req.HeroID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("Hero is no longer available")
	}
	h := &owner.Heroes[idx]
	switch req.Action {
	case "steal":
		if owner == self || !h.Tank {
			return fmt.Errorf("Hero cannot be stolen")
		}
		moved := *h
		moved.ID = self.NextID
		self.NextID++
		moved.Tank = false
		self.Heroes = append(self.Heroes, moved)
		owner.Heroes = append(owner.Heroes[:idx], owner.Heroes[idx+1:]...)
	case "sell":
		self.Heroes = append(self.Heroes[:idx], self.Heroes[idx+1:]...)
		self.Coins += 25
	case "upgrade":
		if self.Coins < 20 {
			return fmt.Errorf("Not enough coins")
		}
		self.Coins -= 20
		h.Level++
	case "breed":
		if self.Coins < 30 {
			return fmt.Errorf("Not enough coins")
		}
		partner := false
		for _, p := range self.Heroes {
			if p.ID == req.PartnerID && p.ID != h.ID {
				partner = true
			}
		}
		if !partner {
			return fmt.Errorf("Breeding partner is no longer available")
		}
		child := TankHero{ID: self.NextID, Kind: h.Kind, Level: 1, Hunger: 100, RewardAt: now + 14}
		self.NextID++
		self.Coins -= 30
		self.Heroes = append(self.Heroes, child)
	case "place":
		count := 0
		for _, p := range self.Heroes {
			if p.Tank {
				count++
			}
		}
		if !h.Tank && count >= 5 {
			return fmt.Errorf("Tank is full")
		}
		h.Tank = !h.Tank
	case "reward":
		if !h.Tank || now < h.RewardAt {
			return fmt.Errorf("Reward is not ready")
		}
		h.RewardAt = now + 14
		self.Coins += 5
	default:
		return fmt.Errorf("Unknown tank action")
	}
	return nil
}
