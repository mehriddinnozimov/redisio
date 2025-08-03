package main

import (
	"fmt"
	"net"
	"os"
)

var _ = net.Listen
var _ = os.Exit
var PORT = 6379
var HOST = "0.0.0.0"
var PROTOCOL = "tcp"

var PONG_MESSAGE = []byte("+PONG\r\n")

func main() {
	fmt.Println("Logs from your program will appear here!")

	l, err := net.Listen(PROTOCOL, fmt.Sprintf("%s:%d", HOST, PORT))
	if err != nil {
		fmt.Printf("Failed to bind to %s:%d: %s\n", HOST, PORT, err.Error())
		os.Exit(1)
	}

	fmt.Printf("Listening %s:%d\n", HOST, PORT)

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			continue
		} else {
			conn.Write(PONG_MESSAGE)
		}
	}
}
