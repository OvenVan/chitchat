package main

import (
	"errors"
	"fmt"
	"github.com/ovenvan/chitchat/common"
	"time"
	"unsafe"
)

type Test struct {
	Id    int
	Value int
}

func Creadfunc(str []byte, c *common.Client) error {
	var t = *(**Test)(unsafe.Pointer(&str))
	fmt.Println("from ", c.GetRemoteAddr(), ": data is : ", t)
	if t.Value < 100 {
		return c.Write(Test{Id: t.Id + 1, Value: t.Value * 3})
	} else {
		c.Close()
	}
	return nil
}

func Sreadfunc(str []byte, s *common.Server) error {
	time.Sleep(time.Second)
	var t = *(**Test)(unsafe.Pointer(&str))
	fmt.Println("from ", s.GetRemoteAddr(), ": data is : ", t)
	if t.Value < 100 {
		return s.Write(Test{Id: t.Id + 1, Value: t.Value * 3})
	} else {
		s.Close(s.GetRemoteAddr())
		return errors.New("Return From Server")
	}
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
	var input byte
	server := common.NewServer("127.0.0.1:8085", '\n', Sreadfunc)
	fmt.Println(server.Listen())
	go DaemonListen(server.ErrChan())

	client1 := common.NewClient("127.0.0.1:8085", '\n', Creadfunc)
	_ = client1.Dial()
	go DaemonListen(client1.ErrChan())

	client2 := common.NewClient("127.0.0.1:8085", '\n', Creadfunc)
	_ = client2.Dial()
	go DaemonListen(client2.ErrChan())
	fmt.Println("write err:", client1.Write(Test{Id: 0, Value: 2}))
	fmt.Println("write err:", client2.Write(Test{Id: 0, Value: 3}))
	_, _ = fmt.Scan(&input)
	server.Cut()
	time.Sleep(time.Second)
}
