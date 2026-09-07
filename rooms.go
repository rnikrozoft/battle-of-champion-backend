package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"

	"github.com/heroiclabs/nakama-common/runtime"
)

// This directory belongs to the single Nakama node in docker-compose.yml.
// Serialize allocation so concurrent creates cannot claim the same code.
var rooms = struct {
	sync.Mutex
	ids map[string]string
}{ids: make(map[string]string)}

func validRoomCode(code string) bool {
	if len(code) != 4 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func releaseRoom(code string) {
	rooms.Lock()
	defer rooms.Unlock()
	delete(rooms.ids, code)
}

func resolveRoom(ctx context.Context, nk runtime.NakamaModule, uid, payload string) (string, error) {
	var request struct {
		Code string `json:"room_code"`
	}
	if json.Unmarshal([]byte(payload), &request) != nil || (request.Code != "" && !validRoomCode(request.Code)) {
		return "", runtime.NewError("Enter a 4-digit room code", 3)
	}
	rooms.Lock()
	defer rooms.Unlock()
	reply := func(code, id string) (string, error) {
		data, _ := json.Marshal(map[string]string{"match_id": id, "room_code": code})
		return string(data), nil
	}
	if request.Code != "" {
		id := rooms.ids[request.Code]
		if id == "" {
			return "", runtime.NewError("Room not found", 5)
		}
		match, err := nk.MatchGet(ctx, id)
		if err != nil {
			return "", err
		}
		if match == nil {
			delete(rooms.ids, request.Code)
			return "", runtime.NewError("Room no longer available", 5)
		}
		if match.Size >= playerLimit {
			return "", runtime.NewError("Room full (10 players maximum)", 8)
		}
		return reply(request.Code, id)
	}
	// Reuse an owner's live room, and prune retired rooms before allocating.
	for code, id := range rooms.ids {
		match, err := nk.MatchGet(ctx, id)
		if err != nil {
			return "", err
		}
		if match == nil {
			delete(rooms.ids, code)
			continue
		}
		var label map[string]string
		if match.Label != nil && json.Unmarshal([]byte(match.Label.Value), &label) == nil && label["owner"] == uid {
			return reply(code, id)
		}
	}
	start := rand.Intn(10000)
	for i := 0; i < 10000; i++ {
		code := fmt.Sprintf("%04d", (start+i)%10000)
		if _, used := rooms.ids[code]; used {
			continue
		}
		id, err := nk.MatchCreate(ctx, "survival_arena", map[string]interface{}{"owner": uid, "room_code": code})
		if err != nil {
			return "", err
		}
		rooms.ids[code] = id
		return reply(code, id)
	}
	return "", runtime.NewError("All room codes are in use; try again later", 8)
}

func freeSlot(players []*Actor) int {
	var used [playerLimit]bool
	for _, p := range players {
		if p.Slot >= 0 && p.Slot < playerLimit {
			used[p.Slot] = true
		}
	}
	for slot, occupied := range used {
		if !occupied {
			return slot
		}
	}
	return -1
}

func playerSpawn(slot int) Point {
	// Alternate the two editor-authored spawn points for all ten slots.
	return world.Players[slot%len(world.Players)]
}
