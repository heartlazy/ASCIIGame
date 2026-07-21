package client

import (
	"bufio"
	"net"
)

// Conn is the client's TCP connection to the server, with newline-framed reads.
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

// Send writes a pre-framed command (must already end in '\n').
func (c *Conn) Send(frame string) error {
	_, err := c.conn.Write([]byte(frame))
	return err
}

// ReadLoop reads newline-framed messages and invokes onMsg for each, until the
// connection closes. Mirrors recv_thread_func (client/game.c:655-688). onClose
// runs when the loop exits.
func (c *Conn) ReadLoop(onMsg func(string), onClose func()) {
	defer onClose()
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return
		}
		onMsg(line)
	}
}

// Close closes the connection.
func (c *Conn) Close() error { return c.conn.Close() }
