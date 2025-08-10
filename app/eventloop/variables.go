package eventloop

import "time"

var MAX_EVENTS = 64
var MAX_BYTE = 4096
var TIMEOUT = 60 * time.Second
var EPOOL_TIMEOUT = 1000
var WAIT_UNTIL_CONTINUE = time.Millisecond * 100

const (
	Random LoadBalanceType = iota
	RoundRobin
	LeastConnections
)
