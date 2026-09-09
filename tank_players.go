package main

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/heroiclabs/nakama-common/runtime"
)

// Expose only public account identifiers, never console credentials or account PII.
// The tank RPC serves each listed account's authoritative inventory.
func listTankPlayers(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	uid, _ := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if uid == "" {
		return "", runtime.NewError("Authentication required", 16)
	}
	rows, err := db.QueryContext(ctx, `SELECT id::text, username FROM users
		WHERE id <> '00000000-0000-0000-0000-000000000000' AND id <> $1::uuid
		ORDER BY username, id LIMIT 100`, uid)
	if err != nil {
		logger.Error("List tank players: %v", err)
		return "", runtime.NewError("Cannot load players", 13)
	}
	defer rows.Close()
	type player struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	players := make([]player, 0)
	for rows.Next() {
		var p player
		if err := rows.Scan(&p.UserID, &p.Username); err != nil {
			return "", runtime.NewError("Cannot read player", 13)
		}
		players = append(players, p)
	}
	if rows.Err() != nil {
		return "", runtime.NewError("Cannot read players", 13)
	}
	data, err := json.Marshal(map[string]interface{}{"players": players})
	return string(data), err
}
