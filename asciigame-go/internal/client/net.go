package client

import (
	"bufio"
	"net"

	"github.com/heartlazyli/asciigame/internal/protocol"
)

// Conn is the client's TCP connection to the server, with length-prefixed
// protobuf framing.
type Conn struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial connects to the server at addr (host:port).
func Dial(addr string) (*Conn, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: c, r: bufio.NewReader(c)}, nil
}

// Send writes one frame.
func (c *Conn) Send(f *protocol.Frame) error {
	return protocol.WriteFrame(c.conn, f)
}

// ReadLoop reads frames and invokes onMsg for each, until the connection
// closes. Mirrors recv_thread_func (client/game.c:655-688).
func (c *Conn) ReadLoop(onMsg func(*protocol.Frame), onClose func()) {
	defer onClose()
	for {
		f, err := protocol.ReadFrame(c.r)
		if err != nil {
			return
		}
		onMsg(f)
	}
}

// Close closes the connection.
func (c *Conn) Close() error { return c.conn.Close() }
