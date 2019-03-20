package common

import (
	"net"
	"time"
)

type client struct {
	ipaddr    string
	port      string
	dialDDL   time.Duration
	delimiter byte
	done      chan struct{}
	err       error
	//readfunc  func([]byte) error
}

func (t *client) Dial(){
	var err error
	ipsocket := t.ipaddr + ":" + t.port
	var conn net.Conn
	if t.dialDDL == 0{
		conn, err = net.Dial("tcp", ipsocket)
		if err != nil{
			close(t.done)
		}
	}else{
		conn, err = net.DialTimeout("tcp", ipsocket, t.dialDDL)
	}
_ = conn
}