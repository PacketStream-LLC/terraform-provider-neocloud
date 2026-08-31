package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/provider"
)

// goreleaser 가 -ldflags 로 채운다.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/packetstream-llc/neocloud",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
