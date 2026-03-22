package main

import (
	"fmt"
	"os"

	"github.com/defilantech/infercost/pkg/cli"
)

func main() {
	cmd := cli.NewRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
