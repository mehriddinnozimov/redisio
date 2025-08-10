package main

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"time"

	"github.com/codecrafters-io/redis-starter-go/app/eventloop"
)

func main() {
	var writed = 0
	var closed = 0
	var mu = sync.Mutex{}
	option := eventloop.EventLoopManagerOption{
		Network:   "tcp",
		LoopCount: 4,
		OnError: func(err error, in []byte) {
			fmt.Printf("error occured: %v\n", err)
		},
		EpoolTimeout: 1000,
		MaxEvents:    64,
		OnData: func(in []byte, conn *eventloop.Conn) {
			mu.Lock()
			writed++
			mu.Unlock()
			conn.Write([]byte("hello"))
			rand := rand.IntN(2)
			if rand == 1 {
				mu.Lock()
				closed++
				mu.Unlock()
				conn.Close()

			}
		},
		LoadBalance: eventloop.LeastConnections,
	}
	elManager, err := eventloop.NewEventLoopManager(option)
	if err != nil {
		panic(err)
	}

	err = elManager.Listen("localhost:4000")
	if err != nil {
		panic(err)
	}

	var (
		maxGoroutines int
		maxAlloc      uint64
		maxTotalAlloc uint64
		maxSys        uint64
		maxHeapSys    uint64
		maxHeapAlloc  uint64
		maxNumGC      uint32
	)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	ticker2 := time.NewTicker(500 * time.Millisecond)
	defer ticker2.Stop()

	for {
		select {
		case <-ticker2.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)

			if g := runtime.NumGoroutine(); g > maxGoroutines {
				maxGoroutines = g
			}
			if ms.Alloc > maxAlloc {
				maxAlloc = ms.Alloc
			}
			if ms.TotalAlloc > maxTotalAlloc {
				maxTotalAlloc = ms.TotalAlloc
			}
			if ms.Sys > maxSys {
				maxSys = ms.Sys
			}
			if ms.HeapSys > maxHeapSys {
				maxHeapSys = ms.HeapSys
			}
			if ms.HeapAlloc > maxHeapAlloc {
				maxHeapAlloc = ms.HeapAlloc
			}
			if ms.NumGC > maxNumGC {
				maxNumGC = ms.NumGC
			}

		case t := <-ticker.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			g := runtime.NumGoroutine()
			runtime.GC()

			fmt.Printf("=== Max Runtime Stats (every 4 sec) at %v ===\n", t)
			fmt.Printf("Max Goroutines: %d\n", maxGoroutines)
			fmt.Printf("Max Alloc = %v MiB\n", bToMb(maxAlloc))
			fmt.Printf("Max TotalAlloc = %v MiB\n", bToMb(maxTotalAlloc))
			fmt.Printf("Max Sys = %v MiB\n", bToMb(maxSys))
			fmt.Printf("Max HeapSys = %v MiB\n", bToMb(maxHeapSys))
			fmt.Printf("Max HeapAlloc = %v MiB\n", bToMb(maxHeapAlloc))
			fmt.Printf("Max NumGC = %d\n", maxNumGC)

			fmt.Printf("Current Goroutines: %d\n", g)
			fmt.Printf("Current Alloc = %v MiB\n", bToMb(ms.Alloc))
			fmt.Printf("Current TotalAlloc = %v MiB\n", bToMb(ms.TotalAlloc))
			fmt.Printf("Current Sys = %v MiB\n", bToMb(ms.Sys))
			fmt.Printf("Current HeapSys = %v MiB\n", bToMb(ms.HeapSys))
			fmt.Printf("Current HeapAlloc = %v MiB\n", bToMb(ms.HeapAlloc))
			fmt.Printf("Current NumGC = %d\n", ms.NumGC)

			fmt.Println("Current Writed", writed)
			fmt.Println("Current Closed", closed)

			elManager.Stats()

			fmt.Println()
		}
	}

}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
