package main

import "github.com/cloudflare/cloudflare-go/v6/zones"

func zoneFixture() zones.Zone {
	return zones.Zone{
		ID:   "zone-1",
		Name: "example.com",
		Account: zones.ZoneAccount{
			ID:   "acc-1",
			Name: "Primary",
		},
	}
}
