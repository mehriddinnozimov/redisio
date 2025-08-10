package eventloop

import (
	"fmt"
	"syscall"
	"time"
)

type Conn struct {
	fd           int
	lastReadTime time.Time
	el           *EventLoop
	ID           int
	buf          []byte
	closed       int32
}

func (conn *Conn) Write(out []byte) (int, error) {
	n, err := syscall.Write(conn.fd, out)
	return n, err
}

func (conn *Conn) Close() {
	fmt.Println("closing conn, fd: ", conn.fd)
	conn.el.pushOp(pendingOp{conn: conn, typ: opRemove})
}
