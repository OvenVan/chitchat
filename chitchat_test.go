package chitchat

import (
	"testing"
	"time"
)

func TestSlave_Register(t *testing.T) {
	_ = NewMaster("127.0.0.1:12345").Listen()
	var node NodeRoler
	for i := 0; i < 1; i++ {
		node = NewNode("127.0.0.1:12345")
		_ = node.Register()
	}

	time.Sleep(time.Second * 10)
	node.Leave()
	time.Sleep(time.Hour)
}
