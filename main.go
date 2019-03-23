package main

import (
	"errors"
	"fmt"
	"github.com/ovenvan/chitchat/common"
	"net"
	"time"
)

type Test struct {
	Id    int
	Value int
}

func readfunc(s []byte, c net.Conn) error {
	//fmt.Println(string(s))
	for _, v := range s {
		fmt.Print(string(v))
		time.Sleep(time.Millisecond * 10)
	}
	fmt.Println()
	return errors.New("Custom err")
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
	//data := common.Struct2byte(Test{3, 100})
	//var ptestStruct *Test = *(**Test)(unsafe.Pointer(&data))
	//fmt.Println("ptestStruct.data is : ", ptestStruct)
	x := common.NewServer("127.0.0.1:8085",
		0,
		readfunc)
	fmt.Println(x.Listen())
	go DaemonListen(x.ErrChan())
	c1, _ := net.Dial("tcp", "127.0.0.1:8085")
	c2, _ := net.Dial("tcp", "127.0.0.1:8085")
	c2.Write([]byte("hello world c2"))
	c1.Write([]byte("hello world c1"))
	c1.Close()
	c2.Close()
	time.Sleep(time.Millisecond * 1000)
	x.Cut() //situation:还在等待阅读时被强行关闭
	time.Sleep(time.Hour)
}
