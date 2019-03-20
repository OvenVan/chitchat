package main

import (
	"github.com/ovenvan/chitchat/common"
	"net"
	"time"
)

type Test struct {
	Id    int
	Value int
}

func readfunc(s []byte) error {
	return nil
}

func main() {
	//data := common.Struct2byte(Test{3, 100})
	//var ptestStruct *Test = *(**Test)(unsafe.Pointer(&data))
	//fmt.Println("ptestStruct.data is : ", ptestStruct)

	x := common.NewServer("127.0.0.1",
		"8085",
		0,
		0,
		'\n',
		readfunc)
	x.Listen()
	time.Sleep(time.Second)
	net.Dial("tcp","127.0.0.1:8085")
	time.Sleep(time.Second)
	x.Cut()
	time.Sleep(time.Hour)
}
