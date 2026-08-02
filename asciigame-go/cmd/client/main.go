// Command client runs the ASCII Battle Royale terminal client (Go port).
//
// Usage: client [host] [tcp_port] [http_port]
//   Defaults: 127.0.0.1 8888 8080
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/heartlazyli/asciigame/internal/client"
)

func main() {
	host := "127.0.0.1"
	tcpPort := 8888
	httpPort := 8080
	if len(os.Args) > 1 {
		host = os.Args[1]
	}
	if len(os.Args) > 2 {
		if p, err := strconv.Atoi(os.Args[2]); err == nil {
			tcpPort = p
		}
	}
	if len(os.Args) > 3 {
		if p, err := strconv.Atoi(os.Args[3]); err == nil {
			httpPort = p
		}
	}

	httpBase := fmt.Sprintf("http://%s:%d", host, httpPort)
	tcpAddr := fmt.Sprintf("%s:%d", host, tcpPort)
	ui := client.NewUI(httpBase, tcpAddr)
	if err := ui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "client error: %v\n", err)
		os.Exit(1)
	}
}
