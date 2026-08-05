package main

import (
	"github.com/zane-byte-dev/atm/internal/cmd"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// version is injected at build time via -X main.version (see Makefile and
// .goreleaser.yaml). The fallback is deliberately not a real version number: a
// binary built without ldflags should say so rather than claim a release it is
// not.
var version = "dev"

func main() {
	config.LoadConfig()
	store.SetPricing(config.Pricing)
	cmd.SetVersion(version)
	cmd.Execute()
}
