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
	readfunc  func([]byte, net.Conn) error
	//unchangable data

	errDone chan Errsocket
	closed  bool
	//under protected data
	mu sync.Mutex

	listener   net.Listener
	cancelfunc context.CancelFunc
}

type handleconn struct {
	c        net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc func([]byte, net.Conn) error
}

type reader struct {
	c          net.Conn
	d          byte
	pmu        *sync.Mutex
	strReqChan chan<- []byte
}

type eDer struct {
	eD     chan Errsocket
	closed *bool
	eDT    chan Errsocket
	mu     *sync.Mutex
	pmu    *sync.Mutex
}

func NewServer(
	ipaddrsocket string,
	delim byte, readfunc func([]byte, net.Conn) error) *server {
	return &server{
		ipaddr:    ipaddrsocket,
		readDDL:   0,
		writeDDL:  0,
		delimiter: delim,
		errDone:   make(chan Errsocket),
		closed:    false,
		readfunc:  readfunc,
	}
}

func (s *server) SetDeadLine(rDDL time.Duration, wDDL time.Duration) {
	s.readDDL, s.writeDDL = rDDL, wDDL
}

func (s *server) ErrDone() <-chan Errsocket {
	return s.errDone
}

/*
will not wait for the rest of goroutines' error message.
make sure all connections has exited successfully before doing this
*/
func (s *server) Cut() {
	//fmt.Println("Start Cut")
	//defer fmt.Println("->Cut")
	s.mu.Lock()
	err := s.listener.Close()
	if err != nil {
		s.errDone <- Errsocket{err, s.ipaddr}
	}
	s.closed = true
	close(s.errDone)
	s.mu.Unlock()
	s.cancelfunc()
}

func errDiversion(eD *eDer) {
	//when upstream channel is closed(by closed flag),
	//following data will be received but discarded
	//when eDT channel has closed, this goroutine will exit
	fmt.Println("Start eD")
	defer fmt.Println("->eD quit")
	for {
		err, ok := <-eD.eDT
		if !ok {
			return
		}
		if !*eD.closed {
			if eD.pmu != nil {
				eD.pmu.Lock() //send to upstream channel
			}
			eD.eD <- err
		}
		eD.mu.Unlock()
	}
}

func (s *server) Listen() error { //Notifies the consumer when an error occurs ASYNCHRONOUSLY
	listener, err := net.Listen("tcp", s.ipaddr)
	if err != nil {
		return err
	}
	s.listener = listener
	eDT := make(chan Errsocket)
	go errDiversion(&eDer{
		eD:     s.errDone,
		closed: &s.closed,
		eDT:    eDT,
		mu:     &s.mu,
		pmu:    nil,
	})
	go handleListen(s, listener, eDT)
	return nil
}

func handleListen(s *server, l net.Listener, eDT chan Errsocket) {
	fmt.Println("Start hL")
	defer fmt.Println("->hL quit")

	var ctx, cfunc = context.WithCancel(context.Background())
	defer close(eDT)

	s.cancelfunc = cfunc

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := l.Accept()
			if err != nil {
				s.mu.Lock()
				eDT <- Errsocket{err, s.ipaddr}
				return
			}

			heDT := make(chan Errsocket)

			if s.readDDL != 0 {
				conn.SetReadDeadline(time.Now().Add(s.readDDL))
			}
			if s.writeDDL != 0 {
				conn.SetWriteDeadline(time.Now().Add(s.writeDDL))
			}

			go errDiversion(&eDer{
				eD:     s.errDone,
				closed: &s.closed,
				eDT:    heDT,
				mu:     &s.mu,
				pmu:    nil,
			})

			go handleConn(&handleconn{
				c:        conn,
				d:        s.delimiter,
				readfunc: s.readfunc,
				mu:       &s.mu,
			}, ctx, heDT)
		}
	}
}

func handleConn(h *handleconn, parentctx context.Context, eDT chan Errsocket) {
	fmt.Println("Start hC")
	defer fmt.Println("->hC quit")
	ctx, _ := context.WithCancel(parentctx) //TODO: if MAXLIVETIME is needed.
	strReqChan := make(chan []byte)

	defer func() {
		err := h.c.Close()
		<-strReqChan
		if err != nil {
			h.mu.Lock()
			eDT <- Errsocket{err, h.c.RemoteAddr().String()}
		}
		close(eDT)
	}()

	go read(&reader{
		c:          h.c,
		d:          h.d,
		pmu:        h.mu,
		strReqChan: strReqChan,
	}, eDT)

	for {
		select {
		case <-ctx.Done(): //quit manually
			return
		case strReq, ok := <-strReqChan: //read a data slice successfully
			if !ok {
				return //EOF && d!=0
			}
			h.mu.Lock()                    //s.mu
			err := h.readfunc(strReq, h.c) //requires a lock from hL
			h.mu.Unlock()
			if err != nil {
				h.mu.Lock()
				eDT <- Errsocket{err, h.c.RemoteAddr().String()}
			}
		}
	}
}

/*
1. No delimiter with closed connection:						DO readfunc with string read.
2. Delimiter with closed connection(no delimiter found): 	EOF warning.
3. No delimiter with healthy connection:					waiting for closed.
4. Delimiter with healthy connection:						waiting for delimiter to DO readfunc
5. No delimiter with CUT: 									DO NOTHING.
6. Delimiter with CUT: 										DO NOTHING.
*/
func read(r *reader, eDT chan Errsocket) {
	fmt.Println("Start read")
	defer fmt.Println("->read quit")
	defer func() {
		close(r.strReqChan)
	}()
	readBytes := make([]byte, 1)
	var buffer bytes.Buffer
	for {
		_, err := r.c.Read(readBytes)
		if err != nil {
			if r.d == 0 {
				r.strReqChan <- buffer.Bytes()
			} else {
				r.pmu.Lock()
				eDT <- Errsocket{err, r.c.RemoteAddr().String()}
			}
			return
		}
		readByte := readBytes[0]
		if readByte == r.d {
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
