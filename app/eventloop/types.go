package eventloop

type CB func(in []byte, conn *Conn)
type CBErr func(err error, in []byte)
type LoadBalanceType int
