package chitchat

import (
	"errors"
	"fmt"
	"github.com/ovenvan/chitchat/common"
	"time"
)

type MasterRoler interface {
	Listen() error
}
type NodeRoler interface {
	Register() error
	Leave()
}

type Node struct {
	roleMaster bool
	local      ipx
	remote     ipx

	//For Nodes
	leave func()

	//For MasterNode
	registeredIP []string
}

type ipx struct {
	ipaddr string
	ipport string
}

func registerNode(str []byte, s common.ReadFuncer) error {
	if sx := string(str); sx != "" {
		x := s.Addon().(*Node)
		x.registeredIP = append(x.registeredIP, x.remote.ipaddr)
		go daemonHBChecker(x.remote.ipaddr)
		fmt.Println(x.registeredIP)
	}
	return nil
}

func hb4node(str []byte, s common.ReadFuncer) error {
	if sx := string(str); sx == "heartbeat ping" {
		return s.Write("heartbeat pong")
	}
	return errors.New("err message received.")
}
func hb4master(str []byte, s common.ReadFuncer) error {
	if sx := string(str); sx == "heartbeat pong" {
		fmt.Println("heartbeat check succeed.")
		s.Close()
		return nil
	}
	return errors.New("err message received.")
}

func (t *Node) daemonHBListener() error { //for Nodes listen Master's heartbeat check
	s := common.NewServer(t.local.ipaddr+":"+"7939", '\n', hb4node, nil)
	t.leave = s.Cut
	if err := s.Listen(); err != nil {
		return err
	}
	go func() {
		for {
			v, ok := <-s.ErrChan()
			if ok {
				if v.Err.Error() == "err message received" {
					//failedTimes++
				}
			} else {
				fmt.Println("HBL Listen closed.") //when to close: Cut.
				return
			}
		}
	}()
	return nil
}
func daemonHBChecker(ipAddr string) {
	i := time.NewTicker(5 * time.Second)
	failedTimes := 0
	var c *common.Client
	for {
		select {
		case <-i.C:
			fmt.Println("-----------------------------------")
			c = common.NewClient(ipAddr+":"+"7939", '\n', hb4master, nil)
			c.SetDeadLine(2 * time.Second)
			if err := c.Dial(); err != nil {
				//TODO: Failed once.
				failedTimes++
				break
			}
			go func() {
				for {
					v, ok := <-c.ErrChan()
					if ok {
						if v.Err.Error() == "err message received" {
							failedTimes++
						}
					} else {
						fmt.Println("HBC Listen closed.")
						return
					}
				}
			}()
			if err := c.Write("heartbeat ping"); err != nil {
				failedTimes++
				break
			}
		}
		if failedTimes > 0 {
			//TODO: this connection is failed.
			fmt.Println(ipAddr+"failed time: ", failedTimes)
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
	}
}

func (t *Node) Listen() error {
	server := common.NewServer(t.local.ipaddr+":"+t.local.ipport, 0, registerNode, t)
	if err := server.Listen(); err != nil {
		return err
	}
	return nil
}

func (t *Node) Register() error {
	slave := common.NewClient(t.remote.ipaddr+":"+t.remote.ipport, 0, nil, nil)
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

func (t *Node) Leave() {
	t.leave()
}
