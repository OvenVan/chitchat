package common

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"unsafe"
)

var closedchan = make(chan struct{})

type ReadFuncer interface {
	GetRemoteAddr() string
	GetLocalAddr() string
	GetConn() net.Conn
	Close()
	Write(interface{}) error
	Addon() interface{}
}

func init() {
	close(closedchan)
}

type eDinitfunc func(eC chan Errsocket)

type Errsocket struct {
	Err  error
	Addr string
}

type reader struct {
	conn       net.Conn
	d          byte
	mu         *sync.Mutex
	strReqChan chan<- []byte
}

type eDer struct {
	eU chan Errsocket
	//why it's a value: this flag will only be modified by CUT, and only used by eD.
	//*eDer.closed is a pointer. closed flag will not be copied.
	//Warning: DONOT copy closed flag separately.
	closed bool
	//why it's a pointer: mutex will be copied separately. make sure the replicas are the same.
	mu  *sync.Mutex
	pmu *sync.Mutex
}

type sliceMock struct {
	addr uintptr
	len  int
	cap  int
}

func errDiversion(eD *eDer) func(eC chan Errsocket) {
	//when upstream channel(uC) is closed(detected by closed flag),
	//following data will be received but discarded
	//when eC channel has closed, this goroutine will exit
	return func(eC chan Errsocket) {
		fmt.Println("Start eD")
		defer fmt.Println("->eD quit")
		for {
			err, ok := <-eC
			if !ok {
				return // consider multi-conn in one uC, i cannot close uC now
			}
			if !eD.closed {
				if eD.pmu != nil {
					eD.pmu.Lock() //send to upstream channel
				}
				eD.eU <- err
			}
			eD.mu.Unlock()
		}
	}
}

/*
Server:
Delimiter with remoted closed connection:         EOF warning.
Delimiter with local   closed connection:         used of a closed network warning.
Delimiter with healthy connection:                waiting for delimiter to DO readfunc
No delimiter with remoted closed connection:      DO readfunc with string read.
No delimiter with local closed:                   DO NOTHING.(Strange)
No delimiter with healthy connection:             waiting for closed.
*/
func read(r *reader, eC chan Errsocket) {
	fmt.Println("Start read", r.conn.LocalAddr(), "->", r.conn.RemoteAddr())
	defer fmt.Println("->read quit", r.conn.LocalAddr(), "->", r.conn.RemoteAddr())
	defer func() {
		close(r.strReqChan)
	}()
	readBytes := make([]byte, 1)
	for {
		var buffer bytes.Buffer
		for {
			_, err := r.conn.Read(readBytes)
			if err != nil {
				//fmt.Println(err.Error())
				if r.d == 0 {
					r.strReqChan <- buffer.Bytes()
				} else {
					r.mu.Lock()
					eC <- Errsocket{err, r.conn.RemoteAddr().String()}
				}
				return
			}
			readByte := readBytes[0]
			if r.d != 0 && readByte == r.d {
				break
			}
			buffer.WriteByte(readByte)
		}
		if r.d == '\n' && buffer.Bytes()[len(buffer.Bytes())-1] == '\r' {
			r.strReqChan <- buffer.Bytes()[:len(buffer.Bytes())-1]
		} else {
			r.strReqChan <- buffer.Bytes()
		}
	}
}

//it doesn't seem good
func Write(c net.Conn, i interface{}, d byte) error {
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
	//fmt.Println(data)
	_, err := c.Write(data)
	return err
}

func struct2byte(t interface{}) []byte {
	var testStruct = &t
	Len := unsafe.Sizeof(*testStruct)

	p := reflect.New(reflect.TypeOf(t))
	p.Elem().Set(reflect.ValueOf(t))
	testBytes := &sliceMock{
		addr: p.Elem().UnsafeAddr(),
		cap:  int(Len),
		len:  int(Len),
	}
	return *(*[]byte)(unsafe.Pointer(testBytes))
}
