package main

import (
	"fmt"
	"log/slog"
	"os"
)

var (
	logger         = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	steamAppId = "294420"
	steamDepotId = "294422"
)

func checkForUpdates() error {
	apiKey := os.Getenv(envSteamApiKey)
	if apiKey == "" {
		return fmt.Errorf("env var %s unset", envSteamApiKey)
	


	return nil
}

func main() {
	err := checkForUpdates()

	code := 0
	if err != nil {
		logger.Error("error while running entrypoint", "msg", err.Error())
		code = 1
	}

	os.Exit(code)
}
