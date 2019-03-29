package chitchat

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"time"
)

type MasterRoler interface {
	Listen() error
	Close() error
}
type NodeRoler interface {
	Register() error
	Leave() error
}

type Node struct {
	roleMaster bool
	local      ipx
	remote     ipx

	//For Nodes/master
	leave func() error

	//For MasterNode
	registeredIP []string
	closesignal  chan struct{}
}

type ipx struct {
	ipaddr string
	ipport string
}

func registerNode(str []byte, s ReadFuncer) error {
	if sx := string(str); sx != "" {
		x := s.Addon().(*Node)
		x.registeredIP = append(x.registeredIP, x.remote.ipaddr)
		go daemonHBChecker(x.remote, x.closesignal)
		fmt.Println(x.registeredIP)
	}
	return nil
}

func hb4node(str []byte, s ReadFuncer) error {
	if sx := string(str); sx == "heartbeat ping" {
		if err := s.Write("heartbeat pong"); err == nil {
			return errors.New("succeed")
		}
		return errors.New("writing data error")
	}
	return errors.New("err message received")
}

func hb4master(str []byte, s ReadFuncer) error {
	defer s.Close()
	if sx := string(str); sx == "heartbeat pong" {
		return errors.New("succeed")
	}
	return errors.New("err message received")
}

func (t *Node) daemonHBListener() error { //for Nodes listen Master's hbc
	fmt.Println("HBListen start.")
	defer fmt.Println("->HBListen quit.")
	s := NewServer(t.local.ipaddr+":"+"7939", '\n', hb4node, nil)
	t.leave = s.Cut
	if err := s.Listen(); err != nil {
		return err
	}
	go func() {
		defer fmt.Println("->HBL err Daemon closed.")
		fmt.Println("HBL err Daemon start.")
		timeout := time.Second * 10
		timer := time.NewTimer(timeout)

		for {
			select {
			case v, ok := <-s.ErrChan():
				if ok {
					if v.Err.Error() == "succeed" { //node sends succeed.
						timer.Reset(timeout)
					}
				} else {
					return
				}
			case <-timer.C:
				err := s.Cut()
				if err != nil {
					println(err)
				}
				fmt.Println("!Found Master is Dead")
				return
				//TODO: Timeout, master is dead.
			}
		}
	}()
	return nil
}

func daemonHBChecker(ip ipx, csignal <-chan struct{}) { //for master check
	defer fmt.Println("->HBChecker quit")
	fmt.Println("HBChecker start.")
	i := time.NewTicker(3 * time.Second)
	failedTimes := 0
	for {
		select {
		case <-csignal:
			return
		case <-i.C:
			fmt.Println("-----------------------------------")
			c := NewClient(ip.ipaddr+":"+"7939", '\n', hb4master, nil)
			c.SetDeadLine(2 * time.Second)
			if err := c.Dial(); err != nil {
				//TODO: Failed once.
				failedTimes++
				break
			}
			go func() {
				fmt.Println("HBC err Daemon start.")
				for {
					v, ok := <-c.ErrChan()
					if ok {
						if v.Err.Error() == "err message received" {
							failedTimes++
						} else if v.Err.Error() == "hbc succeed" {
							failedTimes = 0
						}
					} else {
						fmt.Println("->HBC err Daemon closed.")
						return
					}
				}
			}()
			if err := c.Write("heartbeat ping"); err != nil {
				failedTimes++
				break
			}
		} //break to here

		fmt.Println(ip.ipaddr+":"+ip.ipport+" failed time: ", failedTimes)
		if failedTimes > 3 {
			//TODO: this connection is failed.
			return
		}
	}
}

func iportSplitter(socket string) *ipx {
	flag := false
	s1 := make([]byte, 0)
	for i := 0; i < len(socket); i++ {
		if socket[i] == ':' {
			flag = true
			continue
		}
		if !flag {
			s1 = append(s1, socket[i])
		} else {
			return &ipx{string(s1), socket[i:]}
		}
	}
	return nil
}

func NewNode(remoteAddr string) NodeRoler {
	return &Node{
		roleMaster:   false,
		remote:       *iportSplitter(remoteAddr),
		registeredIP: nil,
	}
}

func NewMaster(ipAddr string) MasterRoler {
	t := iportSplitter(ipAddr)
	return &Node{
		roleMaster:   true,
		local:        *t,
		remote:       *t,
		registeredIP: make([]string, 0),
		closesignal:  make(chan struct{}),
	}
}

func (t *Node) Listen() error {
	server := NewServer(t.local.ipaddr+":"+t.local.ipport, 0, registerNode, t)
	if err := server.Listen(); err != nil {
		return err
	}
	t.leave = server.Cut
	return nil
}

func (t *Node) Register() error {
	slave := NewClient(t.remote.ipaddr+":"+t.remote.ipport, 0, nil, nil)
	if err := slave.Dial(); err != nil {
		return err
	}
	t.local = *iportSplitter(slave.GetLocalAddr())
	if err := slave.Write("hello"); err != nil {
		return err
	}
	slave.Close()
	return t.daemonHBListener()
}

func (t *Node) Leave() error {
	return t.leave()
}

func (t *Node) Close() error {
	err := t.leave()
	if err != nil {
		return err
	}
	close(t.closesignal)
	return nil
}

func mywrite(c net.Conn, i interface{}, d byte) error { //it's just a copy from Write(..)
	if c == nil {
		return errors.New("connection not found")
	}
	var data []byte

	switch reflect.TypeOf(i).Kind() {
	case reflect.String:
		data = []byte(i.(string))
	case reflect.Struct:
		data = struct2byte(i)
	default:
		data = i.([]byte)
	}

	if d != 0 {
		data = append(data, d)
	}
	_, err := c.Write(data)
	return err
}
