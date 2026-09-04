package main

import (
	"fmt"
	"os"

	"github.com/lewtec/tailgopher/internal/engine"
)

func main() {
	if err := engine.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
