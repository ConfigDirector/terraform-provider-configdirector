package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/provider"
)

// version is set via -ldflags at release build time.
var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/ConfigDirector/configdirector",
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
