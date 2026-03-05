package main

import (
	"os"

	"github.com/Perttulands/hermes-relay/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
