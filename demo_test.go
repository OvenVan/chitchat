package chitchat

import (
	"fmt"
	"testing"
	"time"
)

func TestAll(t *testing.T) {
	master := NewMaster("127.0.0.1:12345")
	_ = master.Listen()
	node1 := NewNode("127.0.0.1:12345")
	_ = node1.Register()
	time.Sleep(time.Second * 10)
	//node1.Leave()
	fmt.Println(master.Close())
	fmt.Println("Master closed")
	time.Sleep(time.Second * 20)
}
