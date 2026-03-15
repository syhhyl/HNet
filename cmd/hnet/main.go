package main

import (
	"fmt"
	"os"

	"hnet/internal/app"
	"hnet/internal/client"
)

func main() {
	paths, err := app.ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve paths: %v\n", err)
		os.Exit(1)
	}

	cli := client.New(paths.SocketPath)
	if err := app.RunTUI(cli, paths); err != nil {
		fmt.Fprintf(os.Stderr, "hnet: %v\n", err)
		os.Exit(1)
	}
}
