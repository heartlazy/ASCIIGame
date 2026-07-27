package client

import (
	"fmt"
	"log"
	"os"
	"sync"
)

// debugLogger writes diagnostic lines to the file named in the ASCIIGAME_DEBUG
// environment variable. When the var is unset, all debug calls are no-ops. This
// lets us trace key presses and sends in the TUI (which owns the screen and so
// can't print to stdout).
var (
	dbgOnce sync.Once
	dbg     *log.Logger
)

func initDebug() {
	dbgOnce.Do(func() {
		path := os.Getenv("ASCIIGAME_DEBUG")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		dbg = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	})
}

// debugf logs a formatted diagnostic line if ASCIIGAME_DEBUG is set.
func debugf(format string, args ...any) {
	initDebug()
	if dbg != nil {
		dbg.Output(2, fmt.Sprintf(format, args...))
	}
}
