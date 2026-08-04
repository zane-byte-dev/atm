package main

import (
	"github.com/zane-byte-dev/atm/internal/cmd"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

var version = "0.6.0"

func main() {
	config.LoadConfig()
	store.SetPricing(config.Pricing)
	cmd.SetVersion(version)
	cmd.Execute()
}
