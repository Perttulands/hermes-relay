package main

import (
	"os"

	"github.com/Perttulands/relay/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
