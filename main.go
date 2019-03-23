package main

import (
	"fmt"
	"github.com/ovenvan/chitchat/common"
	"net"
	"time"
	"unsafe"
)

type Test struct {
	Id    int
	Value int
}

func Creadfunc(s []byte, c net.Conn) error {
	var t = *(**Test)(unsafe.Pointer(&s))
	fmt.Println("from ", c.RemoteAddr(), ": data is : ", t)
	if t.Value < 100 {
		return common.Write(c, Test{Id: t.Id + 1, Value: t.Value * 3}, '\n')
	} else {

	}
	return nil
}

func DaemonListen(err <-chan common.Errsocket) {
	for {
		v, ok := <-err
		if ok {
			fmt.Println(v.Addr, v.Err)
		} else {
			fmt.Println("Listen closed.")
			return
		}
	}
}

func main() {
	server := common.NewServer("127.0.0.1:8085", '\n', Creadfunc)
	fmt.Println(server.Listen())
	go DaemonListen(server.ErrChan())

	client1 := common.NewClient("127.0.0.1:8085", '\n', Creadfunc)
	fmt.Println(client1.Dial())
	go DaemonListen(client1.ErrChan())

	client2 := common.NewClient("127.0.0.1:8085", '\n', Creadfunc)
	fmt.Println(client2.Dial())
	go DaemonListen(client2.ErrChan())

	fmt.Println("write err:", client1.Write(Test{Id: 0, Value: 2}))
	fmt.Println("write err:", client2.Write(Test{Id: 0, Value: 3}))
	time.Sleep(time.Second)
	client1.Close()
	client2.Close()
	time.Sleep(time.Second)
	server.Cut()
}
