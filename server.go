package chitchat

import (
	"github.com/ovenvan/chitchat/common"
	"unsafe"
)

type Master struct {
	ipaddr       string
	registeredIP []string
}

type registerstruct struct {
	unused string
}

func registerSlave(str []byte, s common.ReadFuncer) error {
	var rs = *(**registerstruct)(unsafe.Pointer(&str))
	if rs.unused != "" {
		s.Addon().(Master).registeredIP = append(s.Addon().(Master).registeredIP, s.GetRemoteAddr())
	}
	return nil
}

func NewMaster(ipaddr string) *Master {
	return &Master{
		ipaddr:       ipaddr,
		registeredIP: make([]string, 0),
	}
}

func (t *Master) Listen() error {
	server := common.NewServer(t.ipaddr, 0, registerSlave, t)
	err := server.Listen()
	if err != nil {
		return err
	}
	return nil
}
