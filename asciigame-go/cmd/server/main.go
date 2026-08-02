// Command server runs the ASCII Battle Royale game server (Go port).
// It starts two listeners: an HTTP API on :8080 (Gin) for lobby/room
// operations, and a TCP server on :8888 (protobuf) for real-time game I/O.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/server"
)

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(200)
	}

	tcpPort := config.ServerPort // 8888
	httpPort := 8080
	if len(os.Args) > 1 {
		p, err := strconv.Atoi(os.Args[1])
		if err != nil || p <= 0 || p > 65535 {
			fmt.Fprintf(os.Stderr, "Invalid TCP port: %s\n", os.Args[1])
			os.Exit(1)
		}
		tcpPort = p
	}
	if len(os.Args) > 2 {
		p, err := strconv.Atoi(os.Args[2])
		if err != nil || p <= 0 || p > 65535 {
			fmt.Fprintf(os.Stderr, "Invalid HTTP port: %s\n", os.Args[2])
			os.Exit(1)
		}
		httpPort = p
	}

	srv, err := server.New(filepath.FromSlash(config.UsersDB))
	if err != nil {
		log.Fatalf("failed to init server: %v", err)
	}

	srv.RecoverAll()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("=== ASCII Battle Royale Server (Go) ===")

	// Start HTTP API (Gin) in background.
	httpEngine := srv.SetupHTTP()
	httpSrv := &http.Server{Addr: fmt.Sprintf(":%d", httpPort), Handler: httpEngine}
	go func() {
		log.Printf("HTTP API listening on :%d", httpPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	// Start TCP game server (blocks until ctx cancelled).
	tcpAddr := fmt.Sprintf(":%d", tcpPort)
	if err := srv.ListenAndServe(ctx, tcpAddr); err != nil {
		log.Fatalf("TCP server error: %v", err)
	}
	log.Printf("server shut down")
}
