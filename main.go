package main

import (
	"context"
	"flag"
	"log"

	"github.com/AikidoTerraform/terraform-provider-aikido/aikido"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// Set via -ldflags at build time (e.g. -X main.version=1.0.0).
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/AikidoTerraform/aikido",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), aikido.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
