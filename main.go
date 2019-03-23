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

//func readfunc(s []byte, x common.Server) error {
//	var t = *(**Test)(unsafe.Pointer(&s))
//	fmt.Println("from ", , ": data is : ", t)
//	if t.Value<100 {
//		return common.Write(c, Test{Id: t.Id + 1, Value: t.Value * 2}, '\n')
//	}
//	return nil
//}

func Creadfunc(s []byte, c net.Conn) error {
	var t = *(**Test)(unsafe.Pointer(&s))
	fmt.Println("from ", c.RemoteAddr(), ": data is : ", t)
	if t.Value < 100 {
		return common.Write(c, Test{Id: t.Id + 1, Value: t.Value * 2}, '\n')
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

	client := common.NewClient("127.0.0.1:8085", '\n', Creadfunc)
	fmt.Println(client.Dial())
	go DaemonListen(client.ErrChan())

	//common.Write()
	fmt.Println("write err:", client.Write(Test{Id: 0, Value: 1}))
	//client.Close()
	//c1, _ := net.Dial("tcp", "127.0.0.1:8085")
	//c2, _ := net.Dial("tcp", "127.0.0.1:8085")
	//c2.Write([]byte("hello world c2"))
	//c1.Write([]byte("hello world c1"))
	//c1.Close()
	//c2.Close()
	//time.Sleep(time.Millisecond * 1000)
	//server.Cut()
	time.Sleep(time.Hour)
}
