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

type eDinitfunc func(eC chan Errsocket)

type Errsocket struct {
	Err  error
	Addr string
}

type server struct {
	//unchangable data
	ipaddr   string
	readDDL  time.Duration
	writeDDL time.Duration
	//if delimiter is 0, then read until it's EOF
	delimiter byte
	readfunc  func([]byte, net.Conn) error

	//under protected data
	eDer
	eDerfunc eDinitfunc

	l          net.Listener
	cancelfunc context.CancelFunc
}

type client struct {
	ipaddr    string
	dialDDL   time.Duration
	delimiter byte

	readfunc func([]byte, net.Conn) error

	eDer
	eDerfunc eDinitfunc

	c          net.Conn
	cancelfunc context.CancelFunc
}

type handleConner struct {
	c        net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc func([]byte, net.Conn) error
}

type reader struct {
	c          net.Conn
	d          byte
	mu         *sync.Mutex
	strReqChan chan<- []byte
}

type eDer struct {
	eU chan Errsocket
	//why it's a value: this flag will only be modified by CUT, and only used by eD.
	//*eDer.closed is a pointer. closed flag will not be copied.
	closed bool
	//why it's a pointer: mutex will be copied separately. make sure the replicas are the same.
	mu  *sync.Mutex
	pmu *sync.Mutex
}

func NewServer(
	ipaddrsocket string,
	delim byte, readfunc func([]byte, net.Conn) error) *server {

	s := &server{
		ipaddr:    ipaddrsocket,
		readDDL:   0,
		writeDDL:  0,
		delimiter: delim,
		eDer: eDer{
			eU:     make(chan Errsocket),
			closed: false,
			mu:     new(sync.Mutex),
			pmu:    nil,
		},
		readfunc: readfunc,
	}
	s.eDerfunc = errDiversion(&s.eDer)
	return s
}

func NewClient(
	ipaddrsocket string,
	delim byte, readfunc func([]byte, net.Conn) error) *client {

	c := &client{
		ipaddr:    ipaddrsocket,
		dialDDL:   0,
		delimiter: delim,
		readfunc:  readfunc,
		eDer: eDer{
			eU:     make(chan Errsocket),
			closed: false,
			mu:     new(sync.Mutex),
			pmu:    nil,
		},
	}
	c.eDerfunc = errDiversion(&c.eDer)
	return c
}

func (s *server) SetDeadLine(rDDL time.Duration, wDDL time.Duration) {
	s.readDDL, s.writeDDL = rDDL, wDDL
}

func (c *client) SetDeadLine(dDDL time.Duration) {
	c.dialDDL = dDDL
}

func (s *server) ErrChan() <-chan Errsocket {
	return s.eU
}

func (c *client) ErrChan() <-chan Errsocket {
	return c.eU
}

/*
will not wait for the rest of goroutines' error message.
make sure all connections has exited successfully before doing this
*/
func (s *server) Cut() {
	//fmt.Println("Start Cut")
	//defer fmt.Println("->Cut")
	s.mu.Lock()

	err := s.l.Close()
	if err != nil {
		s.eU <- Errsocket{err, s.ipaddr}
	}
	s.eDer.closed = true
	s.closed = true
	close(s.eU)
	s.mu.Unlock()
	s.cancelfunc()
}

func (c *client) Close() {
	//TODO: client.close
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
				return
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

func (s *server) Listen() error { //Notifies the consumer when an error occurs ASYNCHRONOUSLY
	listener, err := net.Listen("tcp", s.ipaddr)
	if err != nil {
		return err
	}
	s.l = listener
	eC := make(chan Errsocket)
	go s.eDerfunc(eC)
	go handleListen(s, eC)
	return nil
}

func (c *client) Dial() error {
	var (
		err        error
		conn       net.Conn
		ctx, cfunc = context.WithCancel(context.Background())
		eC         = make(chan Errsocket)
	)

	defer close(eC)

	c.cancelfunc = cfunc

	if c.dialDDL == 0 {
		c.c, err = net.Dial("tcp", c.ipaddr)
	} else {
		c.c, err = net.DialTimeout("tcp", c.ipaddr, c.dialDDL)
	}
	if err != nil {
		return err
	}
	go c.eDerfunc(eC)
	go handleConn(&handleConner{
		c:        conn,
		d:        c.delimiter,
		readfunc: c.readfunc,
		mu:       c.mu,
	}, ctx, eC)

	return nil
}

func handleListen(s *server, eC chan Errsocket) {
	fmt.Println("Start hL")
	defer fmt.Println("->hL quit")

	var ctx, cfunc = context.WithCancel(context.Background())
	defer close(eC)

	s.cancelfunc = cfunc

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := s.l.Accept()
			if err != nil {
				s.mu.Lock()
				eC <- Errsocket{err, s.ipaddr}
				return
			}

			ceC := make(chan Errsocket)

			if s.readDDL != 0 {
				conn.SetReadDeadline(time.Now().Add(s.readDDL))
			}
			if s.writeDDL != 0 {
				conn.SetWriteDeadline(time.Now().Add(s.writeDDL))
			}

			go s.eDerfunc(ceC)

			go handleConn(&handleConner{
				c:        conn,
				d:        s.delimiter,
				readfunc: s.readfunc,
				mu:       s.mu,
			}, ctx, ceC)
		}
	}
}

func handleConn(h *handleConner, parentctx context.Context, eC chan Errsocket) {
	fmt.Println("Start hC")
	defer fmt.Println("->hC quit")
	ctx, _ := context.WithCancel(parentctx) //TODO: if MAXLIVETIME is needed.
	strReqChan := make(chan []byte)

	defer func() {
		err := h.c.Close()
		<-strReqChan
		if err != nil {
			h.mu.Lock()
			eC <- Errsocket{err, h.c.RemoteAddr().String()}
		}
		close(eC)
	}()

	go read(&reader{
		c:          h.c,
		d:          h.d,
		mu:         h.mu,
		strReqChan: strReqChan,
	}, eC)

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
				eC <- Errsocket{err, h.c.RemoteAddr().String()}
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
func read(r *reader, eC chan Errsocket) {
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
				r.mu.Lock()
				eC <- Errsocket{err, r.c.RemoteAddr().String()}
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
