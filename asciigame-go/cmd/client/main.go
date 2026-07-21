// Command client runs the ASCII Battle Royale terminal client (Go port).
//
// Usage: client [host] [port]   (defaults 127.0.0.1:8888)
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/heartlazyli/asciigame/internal/client"
	"github.com/heartlazyli/asciigame/internal/config"
)

func main() {
	host := "127.0.0.1"
	port := config.ServerPort
	if len(os.Args) > 1 {
		host = os.Args[1]
	}
	if len(os.Args) > 2 {
		if p, err := strconv.Atoi(os.Args[2]); err == nil {
			port = p
		}
	}

	ui := client.NewUI(fmt.Sprintf("%s:%d", host, port))
	if err := ui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "client error: %v\n", err)
		os.Exit(1)
	}
}
