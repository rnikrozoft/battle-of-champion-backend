package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// The same generated catalog is used by the shop cards and the server. Prices
// are always looked up here; a client cannot supply its own purchase price.
//
//go:embed shop_catalog.json
var shopJSON []byte

type ShopItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
	Power  int    `json:"power"`
	Health int    `json:"health"`
	Speed  int    `json:"speed"`
	Rarity string `json:"rarity"`
}

var shopCatalog = func() []ShopItem {
	var catalog []ShopItem
	if err := json.Unmarshal(shopJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog
}()

func buyHero(self *TankState, kind string, now int64) error {
	for _, item := range shopCatalog {
		if item.Kind != kind {
			continue
		}
		if self.Coins < item.Price {
			return fmt.Errorf("Not enough coins")
		}
		self.Coins -= item.Price
		self.Heroes = append(self.Heroes, TankHero{ID: self.NextID, Kind: item.Kind, Level: 1, Hunger: 100, Tank: false, RewardAt: now + 14})
		self.NextID++
		return nil
	}
	return fmt.Errorf("Unknown shop character")
}
