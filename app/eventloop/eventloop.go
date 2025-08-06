package eventloop

import (
	"errors"
	"math"
	"math/rand/v2"
	"net"
	"runtime"
	"syscall"
)

type EL struct {
	fd      int
	onData  CB
	OnError CBErr
	network string
	queue   []Conn
}

type Conn struct {
	raw syscall.RawConn
	fd  uintptr
}

type LoadBalanceType int

const (
	Random LoadBalanceType = iota
	RoundRobin
	LeastConnections
)

type ELManager struct {
	eventLoops      []*EL
	onData          CB
	OnError         CBErr
	loadBalanceType LoadBalanceType
	lastUsedIndex   int
	listener        net.Listener
}
type Write func(out []byte)
type CB func(err error, in []byte, write Write)
type CBErr func(err error, in []byte)

type ElManagerOption struct {
	LoopCount   int
	OnError     CBErr
	OnData      CB
	LoadBalance LoadBalanceType
}

func NewELManager(option ElManagerOption) (*ELManager, error) {
	if option.LoopCount <= 1 {
		if option.LoopCount == 0 {
			option.LoopCount = runtime.NumCPU()
		} else {
			return nil, errors.New("LoopCount should be greater or eqeal than 0 (0 - cpu count)")
		}
	}
	elManager := &ELManager{onData: option.OnData, loadBalanceType: option.LoadBalance, lastUsedIndex: -1, OnError: option.OnError}
	for _ = range option.LoopCount {
		el, err := newEL(option.OnData)
		if err != nil {
			elManager.OnError(err, nil)
			break
		}
		elManager.eventLoops = append(elManager.eventLoops, el)
	}
	return elManager, nil
}

func newEL(cb CB) (*EL, error) {
	epfd, err := syscall.EpollCreate1(0)
	return &EL{fd: epfd, onData: cb}, err
}

func (elManager *ELManager) Listen(network string, address string) error {
	l, err := net.Listen(network, address)
	if err != nil {
		return err
	}

	elManager.listener = l

	go elManager.acceptConn()

	return nil
}

func (elManager *ELManager) acceptConn() {
	for {
		conn, err := elManager.listener.Accept()
		if err != nil {
			elManager.onData(err, nil, nil)
			continue
		} else {
			go elManager.handleConn(conn)
		}
	}
}

func (elManager *ELManager) handleConn(conn net.Conn) {
	el := elManager.pick()
	el.Add(conn)
}

func (elManager *ELManager) pick() *EL {
	var index int

	switch elManager.loadBalanceType {
	case Random:
		index = rand.IntN(len(elManager.eventLoops))
	case LeastConnections:
		var min = math.MaxInt
		for elInd, el := range elManager.eventLoops {
			if len(el.queue) < min {
				min = len(el.queue)
				index = elInd
			}
		}
	case RoundRobin:
		elManager.lastUsedIndex = elManager.lastUsedIndex + 1
		if elManager.lastUsedIndex >= len(elManager.eventLoops) {
			elManager.lastUsedIndex = 0
		}
		index = elManager.lastUsedIndex
	}

	return elManager.eventLoops[index]
}

func (el *EL) loop() {
	for _, conn := range el.queue {
		el.onData(nil, nil, func(out []byte) {
			syscall.Write(int(conn.fd), out)
		})
	}
}

func (el *EL) Add(newConn net.Conn) {
	var rawConn syscall.RawConn
	var err error
	if el.network == "tcp" {
		tcpConn, ok := newConn.(*net.TCPConn)
		if !ok {
			if el.OnError != nil {
				el.OnError(errors.New("not a tcp connection"), nil)
			}
			newConn.Close()
			return
		}

		rawConn, err = tcpConn.SyscallConn()
		if err != nil {
			if el.OnError != nil {
				el.OnError(err, nil)
			}
			newConn.Close()
			return
		}
	}

	conn := Conn{raw: rawConn}
	rawConn.Control(func(f uintptr) {
		conn.fd = f
		err := syscall.SetNonblock(int(conn.fd), true)
		if err != nil {
			el.OnError(err, nil)
		}
	})

	event := &syscall.EpollEvent{
		Events: syscall.EPOLLIN | syscall.EPOLLOUT,
		Fd:     int32(conn.fd),
	}

	err = syscall.EpollCtl(el.fd, syscall.EPOLL_CTL_ADD, int(conn.fd), event)
	if err != nil {
		el.OnError(err, nil)
		return
	}

	el.queue = append(el.queue, conn)
	el.continueLoop()
}

func (el *EL) continueLoop() {
	if len(el.queue) > 0 {
		go el.loop()
	}
}
