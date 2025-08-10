package eventloop

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type EventLoopManager struct {
	eventLoops      []*EventLoop
	onData          CB
	onError         CBErr
	loadBalanceType LoadBalanceType
	lastUsedIndex   int
	listener        net.Listener
	listenerFd      int
	network         string
	mu              sync.Mutex
}

type EventLoopManagerOption struct {
	LoopCount    int
	OnError      CBErr
	OnData       CB
	LoadBalance  LoadBalanceType
	MaxEvents    int
	MaxByte      int
	Network      string
	EpoolTimeout int
}

func NewEventLoopManager(option EventLoopManagerOption) (*EventLoopManager, error) {
	if option.LoopCount < 1 {
		if option.LoopCount == 0 {
			option.LoopCount = runtime.NumCPU()
		} else {
			return nil, errors.New("LoopCount should be greater or eqeal than 0 (0 is cpu count)")
		}
	}

	if option.Network != "tcp" {
		return nil, errors.New("only tcp network supported")
	}

	elManager := &EventLoopManager{onData: option.OnData, loadBalanceType: option.LoadBalance, lastUsedIndex: -1, onError: option.OnError, network: option.Network}
	for i := 0; i < option.LoopCount; i++ {
		el, err := newEventLoop(option.OnData, option.OnError, option.MaxEvents, option.MaxByte, option.Network, option.EpoolTimeout)
		if err != nil {
			if elManager.onError != nil {
				elManager.onError(err, nil)
			}
			break
		}
		elManager.eventLoops = append(elManager.eventLoops, el)
	}
	return elManager, nil
}

func (elManager *EventLoopManager) Close() error {
	err := elManager.listener.Close()
	if err != nil {
		return err
	}

	for _, ev := range elManager.eventLoops {
		err := ev.close()
		if err != nil && elManager.onError != nil {
			elManager.onError(err, nil)
		}
	}

	return nil
}

func (elManager *EventLoopManager) Listen(address string) error {
	l, err := net.Listen(elManager.network, address)
	if err != nil {
		return err
	}

	elManager.listener = l

	if elManager.network == "tcp" {
		tcpL, ok := elManager.listener.(*net.TCPListener)
		if !ok {
			return errors.New("listner is not atcp listener")
		}

		rawConn, err := tcpL.SyscallConn()
		if err != nil {
			return err
		}

		err = rawConn.Control(func(fd uintptr) {
			elManager.listenerFd = int(fd)
		})

		if err != nil {
			return err
		}

		flags, err := unix.FcntlInt(uintptr(elManager.listenerFd), unix.F_GETFL, 0)
		if err != nil {
			return err
		}
		flags &^= unix.O_NONBLOCK
		_, err = unix.FcntlInt(uintptr(elManager.listenerFd), unix.F_SETFL, flags)
		if err != nil {
			return err
		}
	} else {
		return errors.New("unsupported network")
	}

	go elManager.acceptConn()
	fmt.Printf("EventLoopManager started. EventLoop's count: %d\n", len(elManager.eventLoops))

	return nil
}

func (elManager *EventLoopManager) acceptConn() {
	fmt.Println("Starting accepting")

	for {
		nfd, _, err := unix.Accept4(elManager.listenerFd, unix.SOCK_CLOEXEC)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err == unix.EBADF {
				return
			}

			if elManager.onError != nil {
				elManager.onError(err, nil)
			}
			break
		}

		if err := unix.SetNonblock(nfd, true); err != nil {
			unix.Close(nfd)
			if elManager.onError != nil {
				elManager.onError(err, nil)
			}
			continue
		}

		elManager.handleConn(nfd)
	}
}

func (elManager *EventLoopManager) handleConn(fd int) {
	el := elManager.pick()
	if el == nil {
		if elManager.onError != nil {
			elManager.onError(errors.New("no event loop found, closing event loop manager"), nil)
		}
		elManager.Close()
		return
	}

	conn := &Conn{
		fd:           fd,
		el:           el,
		lastReadTime: time.Now(),
		buf:          make([]byte, el.maxByte),
	}

	el.pushOp(pendingOp{typ: opAdd, conn: conn})
}

func (elManager *EventLoopManager) Stats() {
	for _, loop := range elManager.eventLoops {
		fmt.Printf("ID: %d, added: %d, removed: %d, closedByConn: %d, size: %d\n", loop.fd, loop.added, loop.removed, loop.closedByConn, len(loop.conns))
	}
}

func (elManager *EventLoopManager) pick() *EventLoop {
	if len(elManager.eventLoops) == 0 {
		return nil
	}
	var index int
	elManager.mu.Lock()
	defer elManager.mu.Unlock()

	switch elManager.loadBalanceType {
	case Random:
		index = rand.IntN(len(elManager.eventLoops))
	case LeastConnections:
		var min = math.MaxInt
		for elInd, el := range elManager.eventLoops {
			connCount := el.connCount()
			if connCount < min {
				min = connCount
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
