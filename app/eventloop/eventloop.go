package eventloop

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type EventLoop struct {
	fd           int
	onData       CB
	onError      CBErr
	network      string
	conns        map[int32]*Conn
	maxEvents    int
	maxByte      int
	lastConnId   int
	epoolTimeout int
	removed      int
	closedByConn int
	isClosed     int32
	added        int
	wakeFd       int
	pendingOps   []pendingOp
	mu           sync.Mutex
}

type operationType int

const (
	opAdd operationType = iota
	opRemove
)

type pendingOp struct {
	typ  operationType
	conn *Conn
}

func newEventLoop(cb CB, onError CBErr, maxEvents int, maxByte int, network string, epoolTimeout int) (*EventLoop, error) {
	if maxEvents < 1 {
		maxEvents = MAX_EVENTS
	}

	if maxByte < 1 {
		maxByte = MAX_BYTE
	}

	if epoolTimeout == 0 || epoolTimeout < -1 {
		epoolTimeout = EPOOL_TIMEOUT
	}

	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}

	wakeFd, err := unix.Eventfd(0, unix.EFD_NONBLOCK)
	if err != nil {
		return nil, err
	}

	wakeEvent := &unix.EpollEvent{
		Events: syscall.EPOLLIN,
		Fd:     int32(wakeFd),
	}
	if err := unix.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, wakeFd, wakeEvent); err != nil {
		return nil, err
	}

	el := &EventLoop{fd: epfd, onData: cb, wakeFd: wakeFd, onError: onError, maxEvents: maxEvents, maxByte: maxByte, network: network, conns: make(map[int32]*Conn), pendingOps: []pendingOp{}, epoolTimeout: epoolTimeout}

	go el.loop()

	return el, nil
}

func (el *EventLoop) close() error {
	el.isClosed = 1
	errs := []error{}
	for _, conn := range el.conns {
		err := el.remove(conn)
		if err != nil && el.onError != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 && el.onError != nil {
		for _, err := range errs {
			el.onError(err, nil)
		}
	}

	return unix.Close(el.fd)
}

func (el *EventLoop) handleReadEvent(conn *Conn) {
	for {
		if atomic.LoadInt32(&conn.closed) == 1 {
			return
		}
		n, err := unix.Read(conn.fd, conn.buf)
		if n == 0 {
			el.remove(conn)
			break
		}
		if err != nil {
			if err == unix.EAGAIN || err == unix.EBADF {
				break
			}

			if el.onError != nil {
				el.onError(err, conn.buf)
			}
			break
		}

		conn.lastReadTime = time.Now()

		if el.onData != nil {
			el.onData(conn.buf[:n], conn)
		}
	}
}

func (el *EventLoop) loop() {
	events := make([]unix.EpollEvent, el.maxEvents)
	for {
		if atomic.LoadInt32(&el.isClosed) == 1 {
			fmt.Printf("Loop (%d) is closed, breaking\n", el.fd)
			break
		}
		n, err := unix.EpollWait(el.fd, events, el.epoolTimeout)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if el.onError != nil {
				el.onError(err, nil)
			}
		}

		shouldRemove := []*Conn{}
		readyConns := make([]*Conn, 0, n)
		for i := 0; i < n; i++ {
			fd := events[i].Fd

			if int(fd) == el.wakeFd {
				var buf [8]byte
				unix.Read(el.wakeFd, buf[:])

				pendingOps := el.pendingOps
				el.mu.Lock()
				el.pendingOps = nil
				el.mu.Unlock()
				for _, pendingOp := range pendingOps {
					switch pendingOp.typ {
					case opAdd:
						el.add(pendingOp.conn)
					case opRemove:
						el.closedByConn++
						el.remove(pendingOp.conn)
					}
				}

				el.pendingOps = el.pendingOps[:0]

				continue
			}

			if conn, ok := el.conns[fd]; ok {
				if events[i].Events&(unix.EPOLLHUP|unix.EPOLLRDHUP|unix.EPOLLERR) != 0 {
					shouldRemove = append(shouldRemove, conn)
					continue
				}

				readyConns = append(readyConns, conn)
			}
		}

		for _, conn := range readyConns {
			el.handleReadEvent(conn)
		}

		for _, conn := range shouldRemove {
			if el.onError != nil {
				el.onError(errors.New("connection closed"), nil)
			}
			el.remove(conn)
		}
	}
}

func (el *EventLoop) remove(conn *Conn) error {
	if conn == nil {
		return errors.New("nil connection")
	}

	if !atomic.CompareAndSwapInt32(&conn.closed, 0, 1) {
		return nil
	}

	el.removed = el.removed + 1
	delete(el.conns, int32(conn.fd))

	err := unix.EpollCtl(el.fd, unix.EPOLL_CTL_DEL, conn.fd, nil)
	if err != nil && el.onError != nil {
		el.onError(fmt.Errorf("epoll ctl del error: %w", err), nil)
	}

	err = unix.Close(conn.fd)

	return err
}

func (el *EventLoop) add(conn *Conn) {

	event := &syscall.EpollEvent{
		Events: syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLHUP | syscall.EPOLLERR,
		Fd:     int32(conn.fd),
	}

	err := syscall.EpollCtl(el.fd, syscall.EPOLL_CTL_ADD, conn.fd, event)
	if err != nil {

		if el.onError != nil {
			el.onError(err, nil)
		}
		syscall.Close(conn.fd)
		return
	}

	id := el.lastConnId + 1
	el.lastConnId = id
	el.added++

	conn.ID = id

	el.conns[int32(conn.fd)] = conn
}

func (el *EventLoop) pushOp(pendingOp pendingOp) {
	el.mu.Lock()
	el.pendingOps = append(el.pendingOps, pendingOp)
	el.mu.Unlock()
	var one uint64 = 1
	unix.Write(el.wakeFd, (*(*[8]byte)(unsafe.Pointer(&one)))[:])
}

func (el *EventLoop) connCount() int {
	return len(el.conns)
}
