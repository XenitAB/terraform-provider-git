package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/xenitab/terraform-provider-git/internal/provider"
)

var (
	version string = "dev"
)

func main() {
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/xenitab/git",
	}
	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
