package main

import (
	"log/slog"
	"os"

	"github.com/lewtec/tailgopher/internal/engine"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := engine.Run(os.Args[1:]); err != nil {
		slog.Error("tailwind failed", "err", err)
		os.Exit(1)
	}
}
