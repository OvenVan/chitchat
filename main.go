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
	var t = *(**Test)(unsafe.Pointer(&str))
	fmt.Println("from ", s.GetRemoteAddr(), ": data is : ", t)
	if t.Value < 100 {
		return s.Write(Test{Id: t.Id + 1, Value: t.Value * 3})
	} else {
		//s.Close(s.GetRemoteAddr())
		s.CloseCurr()
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
	var input, exit int
	fmt.Println("Numbers for clients:")
	_, _ = fmt.Scan(&input)
	failed := 0
	server := common.NewServer("127.0.0.1:8085", '\n', Sreadfunc)
	server.SetDeadLine(3*time.Second, 0)
	fmt.Println(server.Listen())
	go DaemonListen(server.ErrChan())

	for i := 0; i < input; i++ {
		go func() {
			client := common.NewClient("127.0.0.1:8085", '\n', Creadfunc)
			err := client.Dial()
			_ = err
			if err != nil {
				fmt.Println("Dail Failed!")
				failed++
				//return
			}
			go DaemonListen(client.ErrChan())
			//fmt.Println("write err:", client.Write(Test{Id: 0, Value: 2}))
		}()
	}

	_, _ = fmt.Scan(&exit)

	server.Cut()
	fmt.Println("failed times: ", failed, "/", input, ",", failed*100.0/input, "%")
	time.Sleep(time.Second)
}
