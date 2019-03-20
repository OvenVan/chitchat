package server

import "net"

type client struct {

}

func NewListen(){
	listener, err := net.Listen("tcp", "127.0.0.1:8085")
	if err != nil{
		println(err)
		return
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				println(err)
				break
			}
			go handleRegister(conn)
		}
	}()
}

func handleRegister(conn net.Conn){
	b:=make([]byte, 100)
	n,err:=conn.Read(b)
	if err!= nil{

	}
	content:=string(b[:n])

}

func clientInfo(content string)client{

}