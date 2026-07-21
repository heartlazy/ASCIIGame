// Command server runs the ASCII Battle Royale game server (Go port).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/server"
)

func main() {
	port := config.ServerPort
	if len(os.Args) > 1 {
		p, err := strconv.Atoi(os.Args[1])
		if err != nil || p <= 0 || p > 65535 {
			fmt.Fprintf(os.Stderr, "Invalid port: %s\n", os.Args[1])
			os.Exit(1)
		}
		port = p
	}

	srv, err := server.New(filepath.FromSlash(config.UsersFile))
	if err != nil {
		log.Fatalf("failed to init server: %v", err)
	}

	// Rebuild any in-progress games from the WAL before accepting connections.
	srv.RecoverAll()

	// Graceful shutdown on SIGINT/SIGTERM, mirroring the C signal handler.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("=== ASCII Battle Royale Server (Go) ===")
	addr := fmt.Sprintf(":%d", port)
	if err := srv.ListenAndServe(ctx, addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
	log.Printf("server shut down")
}
