package main

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	const (
		connections = 5000
		serverAddr  = "localhost:4000"
	)

	var wg sync.WaitGroup
	wg.Add(connections)

	concurrencyLimit := make(chan struct{}, 1000) // max 1000 concurrent dials
	var successCount int64

	for i := 0; i < connections; i++ {
		concurrencyLimit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-concurrencyLimit }()

			conn, err := net.DialTimeout("tcp", serverAddr, 3*time.Second)
			if err != nil {
				// Dial failed, just return
				return
			}
			defer conn.Close()

			// Send "ping"
			_, err = conn.Write([]byte("ping"))
			if err != nil {
				return
			}

			// Read response with 3-second timeout
			buf := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				return
			}

			if n > 0 {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	fmt.Printf("Completed %d connections; successful responses: %d\n", connections, successCount)
}
