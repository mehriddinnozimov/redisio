package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
)

var _ = net.Listen
var _ = os.Exit
var PORT = 6379
var HOST = "0.0.0.0"
var PROTOCOL = "tcp"

var PONG_RESPONSE = []byte("+PONG\r\n")
var PING_COMMAND = []byte("PING")
var UNKNOWN_COMMAND_RESPONSE = []byte("UNKNOWN COMMAND RECIEVED")

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
			handleClient(conn)
		}

	}
}

func handleClient(conn net.Conn) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		line, err := readCRLFLine(reader)
		if err != nil {
			if err != io.EOF {
				return nil
			} else {
				return err
			}
		}

		if line[0] == byte('*') || line[0] == byte('$') {
			continue
		}

		cleaned := bytes.TrimSpace(line)
		response := handleRequest(cleaned)
		if len(response) > 0 {
			conn.Write(response)
		}
	}
}

func readCRLFLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return nil, err
	}

	if !bytes.HasSuffix(line, []byte("\r\n")) {
		return nil, fmt.Errorf("line does not end with CRLF: %q", line)
	}

	return bytes.TrimSuffix(line, []byte("\r\n")), nil
}

func handleRequest(cmd []byte) []byte {
	fmt.Printf("Request: %s\n", cmd)
	if bytes.Equal(cmd, PING_COMMAND) || len(cmd) == 0 {
		return PONG_RESPONSE
	} else {
		return bytes.Join([][]byte{UNKNOWN_COMMAND_RESPONSE, []byte(": "), cmd, []byte("\n")}, []byte(""))
	}
}
