package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type roomServer struct {
	runtime.NakamaModule
	matches map[string]*api.Match
}

func (n *roomServer) MatchGet(_ context.Context, id string) (*api.Match, error) {
	return n.matches[id], nil
}
func (n *roomServer) MatchCreate(_ context.Context, _ string, params map[string]interface{}) (string, error) {
	id := fmt.Sprintf("match-%d", len(n.matches))
	label, _ := json.Marshal(params)
	n.matches[id] = &api.Match{MatchId: id, Label: wrapperspb.String(string(label))}
	return id, nil
}

func TestRoomCreateJoinAndConcurrentAllocation(t *testing.T) {
	rooms.ids = make(map[string]string)
	t.Cleanup(func() { rooms.ids = make(map[string]string) })
	nk := &roomServer{matches: make(map[string]*api.Match)}
	ctx := context.Background()
	first, err := resolveRoom(ctx, nk, "owner", "{}")
	if err != nil {
		t.Fatal(err)
	}
	var room map[string]string
	if err := json.Unmarshal([]byte(first), &room); err != nil {
		t.Fatal(err)
	}
	if !validRoomCode(room["room_code"]) {
		t.Fatal(first)
	}
	repeated, err := resolveRoom(ctx, nk, "owner", "{}")
	if err != nil || repeated != first {
		t.Fatal("owner room was not reused", err)
	}
	payload := fmt.Sprintf(`{"room_code":%q}`, room["room_code"])
	joined, err := resolveRoom(ctx, nk, "guest", payload)
	if err != nil || joined != first {
		t.Fatal("join did not resolve original room", err)
	}
	nk.matches[room["match_id"]].Size = 10
	if _, err := resolveRoom(ctx, nk, "guest", payload); err == nil {
		t.Fatal("full room accepted")
	}
	delete(nk.matches, room["match_id"])
	if _, err := resolveRoom(ctx, nk, "guest", payload); err == nil {
		t.Fatal("expired room accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := resolveRoom(ctx, nk, fmt.Sprintf("owner-%d", i), "{}"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(rooms.ids) != 20 {
		t.Fatalf("concurrent code collision: %d", len(rooms.ids))
	}
}
