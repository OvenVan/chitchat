package chitchat

import (
	"testing"
	"time"
)

func TestSlave_Register(t *testing.T) {
	_ = NewMaster("127.0.0.1:12345").Listen()
	for i := 0; i < 1; i++ {
		_ = NewNode("127.0.0.1:12345").Register()
	}
	time.Sleep(time.Hour)
}
