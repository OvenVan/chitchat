package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"sync"
	"time"
	"unsafe"
)

var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

type server struct {
	ipaddr    string
	port      string
	readDDL   time.Duration
	writeDDL  time.Duration
	delimiter byte
	readfunc  func([]byte) error
	//unchangeble data

	errDoneTry chan error
	errDone    chan error
	closed     bool
	//under protected data
	mu sync.Mutex

	listener   net.Listener
	cancelfunc context.CancelFunc
}

type handleconn struct {
	c         net.Conn
	delimiter byte
	done      chan struct{}

	errDone    chan error
	errDoneTry chan error
	closed     bool
	mu         sync.Mutex

	readfunc func([]byte) error
}

func NewServer(
	ipaddr string, port string,
	readDDL time.Duration, writeDDL time.Duration,
	delim byte, readfunc func([]byte) error) *server {
	return &server{
		ipaddr:     ipaddr,
		port:       port,
		readDDL:    readDDL,
		writeDDL:   writeDDL,
		delimiter:  delim,
		errDoneTry: make(chan error),
		errDone:    make(chan error),
		closed:     false,
		readfunc:   readfunc,
	}
}

func (s *server) ErrDone() chan<- error {
	return s.errDone
}

func (s *server) Cut() {
	s.mu.Lock()
	err := s.listener.Close()
	s.closed = true
	if err != nil {
		s.errDone <- err
	}
	close(s.errDone)
	s.mu.Unlock()
	s.cancelfunc()
}

func (s *server) errDiversion() {
	defer fmt.Println("server eD quit")
	for {
		err,ok := <-s.errDoneTry
		if !ok{
			return
		}
		fmt.Println("server eD still working")
		s.mu.Lock()
		if !s.closed {
			s.errDone <- err
		}
		s.mu.Unlock()
	}
}

func (t *handleconn) errDiversion() {
defer fmt.Println("handle eD quit")
	for {
		err,ok := <-t.errDoneTry
		if !ok{
			return
		}
		fmt.Println("handle eD still working")
		t.mu.Lock()
		if !t.closed {
			t.errDone <- err
		}else{
			_ = err
		}
		t.mu.Unlock()
	}
}

func (s *server) Listen() { //异步通知调用者什么时候连接被关闭（出现错误）	//立即结束
	defer fmt.Println("Listen has exited")
	ipsocket := s.ipaddr + ":" + s.port
	listener, err := net.Listen("tcp", ipsocket)
	if err != nil {
		s.errDone <- err
	}
	s.listener = listener
	go s.errDiversion()
	go handleListen(listener, s)
	return
}

func handleListen(l net.Listener, s *server) { //cancelctx
	defer fmt.Println("handleListen quit")
	ctx, cfunc := context.WithCancel(context.Background())
	s.cancelfunc = cfunc

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := l.Accept()
			if err != nil {
				s.errDoneTry <- err //TODO: Does it need a lock to protected
				break
			}
			if s.readDDL != 0 {
				conn.SetReadDeadline(time.Now().Add(s.readDDL))
			}
			if s.writeDDL != 0 {
				conn.SetWriteDeadline(time.Now().Add(s.writeDDL))
			}

			go handleConn(&handleconn{
				c:          conn,
				delimiter:  s.delimiter,
				done:       make(chan struct{}),
				errDone:    s.errDoneTry,
				errDoneTry: make(chan error),
				readfunc:   s.readfunc,
			}, ctx)
		}
	} //close conn at goroutine
}

func handleErr(err error) { //Not used yet, designed for handling errors with channel
	if err == nil { //it will be blocked until user give a signal.
		return
	}

}

func handleConn(h *handleconn, parentctx context.Context) {
	defer fmt.Println("handleC quit")
	var ctx context.Context
	ctx, _ = context.WithCancel(parentctx) //TODO: if MAXLIVETIME is needed.
	strReqChan := make(chan []byte)
	readquit := make(chan struct{})
	go read(h.c, h.delimiter, strReqChan, h.errDoneTry, readquit)
	go h.errDiversion()

	for {
		select {
		case <-ctx.Done(): //手动退出
			h.mu.Lock()
			h.c.Close()
			close(readquit)
			h.closed = true
			close(h.errDone)
			//close(h.errDoneTry)
			h.mu.Unlock()
			return
		case strReq, ok := <-strReqChan: //成功读取一个数据片段
			if !ok {
				return //EOF TODO: check if strReq is empty
			}
			err := h.readfunc(strReq)
			if err != nil {
				h.errDone <- err
			}
			//case err := <-h.errDone: //读取片段时发生错误

		}
	}
}

func read(c net.Conn, d byte, strReqChan chan<- []byte, errChan chan<- error, quit chan struct{}) {
	defer fmt.Println("read quit")
	readBytes := make([]byte, 1)
	for {
		select {
		case <-quit:
			quit <- struct{}{}
			return
		default:
			var buffer bytes.Buffer
			for {
				_, err := c.Read(readBytes)
				if err != nil {
					close(strReqChan)
					if err != io.EOF {
						errChan <- err
						close(errChan)
					}
					return
				}
				readByte := readBytes[0]
				if readByte == d {
					break
				}
				buffer.WriteByte(readByte)
			}
			if d == '\n' && buffer.Bytes()[len(buffer.Bytes())-1] == '\r' {
				strReqChan <- buffer.Bytes()[:len(buffer.Bytes())-1]
			}
			strReqChan <- buffer.Bytes()
		}
	}
}

//---------------------------

type sliceMock struct {
	addr uintptr
	len  int
	cap  int
}

func struct2byte(t interface{}) []byte {
	var testStruct = &t
	Len := unsafe.Sizeof(*testStruct)

	p := reflect.New(reflect.TypeOf(t))
	p.Elem().Set(reflect.ValueOf(t))
	addr := p.Elem().UnsafeAddr()
	testBytes := &sliceMock{
		addr: addr,
		cap:  int(Len),
		len:  int(Len),
	}
	return *(*[]byte)(unsafe.Pointer(testBytes))
}

func Write(t interface{}) error {
	data := struct2byte(t)
	_ = data
	return nil
}
