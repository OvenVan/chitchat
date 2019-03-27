package chitchat

import (
	"testing"
	"time"
)

func TestSlave_Register(t *testing.T) {
	_ = NewMaster("127.0.0.1:12345").Listen()
	var node1 NodeRoler
	node1 = NewNode("127.0.0.1:12345")
	_ = node1.Register()
	//node2 = NewNode("127.0.0.1:12345")
	//_ = node2.Register()
	time.Sleep(time.Second * 10)
	node1.Leave()
	time.Sleep(time.Hour)
}
