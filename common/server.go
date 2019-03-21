package common

import (
	"bytes"
	"context"
	"fmt"
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

type Errsocket struct {
	Err  error
	Addr string
}

type server struct {
	ipaddr   string
	readDDL  time.Duration
	writeDDL time.Duration
	//if delimiter is 0, then read until it's EOF
	delimiter byte
	readfunc  func([]byte) error
	//unchangeble data

	errDoneTry chan Errsocket
	errDone    chan Errsocket
	closed     bool
	//under protected data
	mu sync.Mutex

	listener   net.Listener
	cancelfunc context.CancelFunc
}

type handleconn struct {
	c         net.Conn
	delimiter byte

	errDone    chan Errsocket
	errDoneTry chan Errsocket
	closed     bool //if errDone is closed
	mu         sync.Mutex
	pmu        *sync.Mutex //lock readfunc

	readfunc func([]byte) error
}

func NewServer(
	ipaddrsocket string,
	delim byte, readfunc func([]byte) error) *server {
	return &server{
		ipaddr:     ipaddrsocket,
		readDDL:    0,
		writeDDL:   0,
		delimiter:  delim,
		errDoneTry: make(chan Errsocket),
		errDone:    make(chan Errsocket),
		closed:     false,
		readfunc:   readfunc,
	}
}

func (s *server) SetDeadLine(rDDL time.Duration, wDDL time.Duration) {
	s.readDDL, s.writeDDL = rDDL, wDDL
}

func (s *server) ErrDone() <-chan Errsocket {
	return s.errDone
}

func (s *server) Cut() {
	//fmt.Println("Start Cut")
	//defer fmt.Println("->Cut")
	s.mu.Lock()
	err := s.listener.Close()
	s.closed = true
	if err != nil {
		s.mu.Lock()
		s.errDone <- Errsocket{err, s.ipaddr}
	}
	close(s.errDone)
	s.mu.Unlock()
	s.cancelfunc()
}

func (s *server) errDiversion() {
	//when upstream channel is closed(by closed flag),
	//following data will be received but discarded
	//when eDT channel has closed, this goroutine will exit

	fmt.Println("Start SeD")
	defer fmt.Println("->SeD quit")
	for {
		err, ok := <-s.errDoneTry
		if !ok { //errDoneTry has closed
			return
		}
		if !s.closed { //errDone has NOT closed
			s.errDone <- err
		}
		s.mu.Unlock() //locked at error sender
	}
}

func (h *handleconn) errDiversion() {
	fmt.Println("Start HeD")
	defer fmt.Println("->HeD quit")
	for {
		err, ok := <-h.errDoneTry
		if !ok {
			return
		}
		if !h.closed {
			h.pmu.Lock() //send to upstream channel
			h.errDone <- err
		}
		h.mu.Unlock()
	}
}

func (s *server) Listen() { //Notifies the consumer when an error occurs ASYNCHRONOUSLY
	listener, err := net.Listen("tcp", s.ipaddr)
	if err != nil {
		s.mu.Lock()
		s.errDone <- Errsocket{err, s.ipaddr}
	}
	s.listener = listener
	go s.errDiversion()
	go handleListen(listener, s)
	return
}

func handleListen(l net.Listener, s *server) {
	fmt.Println("Start hL")
	defer fmt.Println("->hL quit")
	var wg sync.WaitGroup
	ctx, cfunc := context.WithCancel(context.Background())
	s.cancelfunc = cfunc
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
			conn, err := l.Accept()
			if err != nil {
				s.mu.Lock()
				s.errDoneTry <- Errsocket{err, s.ipaddr}
				break
			}
			if s.readDDL != 0 {
				conn.SetReadDeadline(time.Now().Add(s.readDDL))
			}
			if s.writeDDL != 0 {
				conn.SetWriteDeadline(time.Now().Add(s.writeDDL))
			}

			h := &handleconn{
				c:          conn,
				delimiter:  s.delimiter,
				errDone:    s.errDoneTry,
				errDoneTry: make(chan Errsocket),
				readfunc:   s.readfunc,
				pmu:        &s.mu,
			}
			go h.errDiversion()
			go handleConn(h, ctx, &wg)
			wg.Add(1)
		}
	}
	wg.Wait() //All handleConn goroutines has exited, s.eDT will not be used anymore
	close(s.errDoneTry)
}

func handleConn(h *handleconn, parentctx context.Context, wg *sync.WaitGroup) {
	fmt.Println("Start hC")
	defer fmt.Println("->hC quit")
	ctx, _ := context.WithCancel(parentctx) //TODO: if MAXLIVETIME is needed.
	strReqChan, readquit, quitcheck := make(chan []byte), make(chan struct{}), make(chan struct{})
	defer func() {
		err := h.c.Close()
		<-quitcheck
		if err != nil {
			h.mu.Lock()
			h.errDoneTry <- Errsocket{err, h.c.RemoteAddr().String()}
		}
		h.mu.Lock()
		h.closed = true
		close(h.errDoneTry)
		h.mu.Unlock()
		wg.Done()
	}()
	go read(h, strReqChan, readquit, quitcheck)
	for {
		select {
		case <-ctx.Done(): //quit manually
			close(readquit)
			return
		case strReq, ok := <-strReqChan: //read a data slice successfully
			if !ok {
				return //EOF && d!=0
			}
			h.pmu.Lock()              //s.mu
			err := h.readfunc(strReq) //requires a lock from hL
			h.pmu.Unlock()
			if err != nil {
				h.mu.Lock()
				h.errDoneTry <- Errsocket{err, h.c.RemoteAddr().String()}
			}
		}
	}
}

func read(h *handleconn, strReqChan chan<- []byte, quit chan struct{}, quitcheck chan struct{}) {
	fmt.Println("Start read")
	defer fmt.Println("->read quit")
	readBytes := make([]byte, 1)
	for {
		select {
		case <-quit:
			quitcheck <- struct{}{}
			return
		default:
			var buffer bytes.Buffer
			for {
				_, err := h.c.Read(readBytes)
				if err != nil {
					if h.delimiter == 0 {
						strReqChan <- buffer.Bytes()
					} else {
						h.mu.Lock()
						h.errDoneTry <- Errsocket{err, h.c.RemoteAddr().String()}
					}
					close(strReqChan)
					quitcheck <- struct{}{}
					return
				}
				readByte := readBytes[0]
				if readByte == h.delimiter {
					break
				}
				buffer.WriteByte(readByte)
			}
			if h.delimiter == '\n' && buffer.Bytes()[len(buffer.Bytes())-1] == '\r' {
				strReqChan <- buffer.Bytes()[:len(buffer.Bytes())-1]
			} else {
				strReqChan <- buffer.Bytes()
			}
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

func WriteStruct(t interface{}, d byte) error {
	data := struct2byte(t)
	if d != 0 {
		data = append(data, d)
	}
	return nil
}

func handleErr(err error) { //Not used yet, designed for handling errors with channel
	if err == nil { //it will be blocked until user give a signal.
		return
	}
}
