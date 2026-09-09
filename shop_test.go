package main

import "testing"

func TestShopPurchases(t *testing.T) {
	if len(shopCatalog) != 17 {
		t.Fatalf("expected every character folder, got %d", len(shopCatalog))
	}
	for _, item := range shopCatalog {
		t.Run(item.Kind, func(t *testing.T) {
			self := initialTank("buyer")
			self.Coins = item.Price
			req := TankRequest{Action: "buy", Kind: item.Kind, Version: 1, SelfVersion: 1}
			if err := applyTankAction(&self, &self, req, 100); err != nil {
				t.Fatal(err)
			}
			hero := self.Heroes[5]
			if self.Coins != 0 || hero.Kind != item.Kind || hero.ID != 6 || hero.Tank || hero.Level != 1 || hero.Hunger != 100 {
				t.Fatalf("incorrect purchase: %+v", self)
			}
			self.Version++
			if err := applyTankAction(&self, &self, req, 100); err == nil {
				t.Fatal("duplicate purchase accepted")
			}
			if len(self.Heroes) != 6 {
				t.Fatal("duplicate created another hero")
			}
		})
	}
}

func TestShopRejectsInvalidPurchases(t *testing.T) {
	self, other := initialTank("buyer"), initialTank("victim")
	req := TankRequest{Action: "buy", Kind: shopCatalog[0].Kind, Version: 1, SelfVersion: 1}
	if err := applyTankAction(&self, &self, req, 100); err == nil {
		t.Fatal("insufficient funds accepted")
	}
	self.Coins = 1000
	req.Kind = "not-a-character"
	if err := applyTankAction(&self, &self, req, 100); err == nil {
		t.Fatal("invalid character accepted")
	}
	req.Kind = shopCatalog[0].Kind
	if err := applyTankAction(&other, &self, req, 100); err == nil {
		t.Fatal("foreign tank purchase accepted")
	}
	if self.Coins != 1000 || len(self.Heroes) != 5 || len(other.Heroes) != 5 {
		t.Fatal("rejected purchase changed accounts")
	}
}
