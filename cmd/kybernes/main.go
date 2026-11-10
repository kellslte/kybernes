package main

import (
	"fmt"
	"os"

	"github.com/kybernes/kybernes/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "kybernes:", err)
		os.Exit(1)
	}
}
