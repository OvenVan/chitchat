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

type ServerReadFunc func([]byte, *Server) error
type ClientReadFunc func([]byte, *Client) error

type Server struct {
	//unchangable data
	ipaddr   string
	readDDL  time.Duration
	writeDDL time.Duration
	//if delimiter is 0, then read until it's EOF
	delimiter byte
	readfunc  ServerReadFunc
	//readfunc  func([]byte, net.Conn) error

	remoteMap map[string]context.CancelFunc

	//under protected data
	eDer
	eDerfunc eDinitfunc

	l          net.Listener
	cancelfunc context.CancelFunc

	currentConn net.Conn
}

type Client struct {
	ipaddr    string
	dialDDL   time.Duration
	delimiter byte

	readfunc ClientReadFunc

	eDer
	eDerfunc eDinitfunc

	c          net.Conn
	cancelfunc context.CancelFunc
}

type hConnerServer struct {
	c        net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc ServerReadFunc
}

type hConnerClient struct {
	c        net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc ClientReadFunc
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
	delim byte, readfunc ServerReadFunc) *Server {

	s := &Server{
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
		remoteMap: make(map[string]context.CancelFunc),
		readfunc:  readfunc,
	}
	s.eDerfunc = errDiversion(&s.eDer)
	return s
}

func NewClient(
	ipremotesocket string,
	delim byte, readfunc ClientReadFunc) *Client {

	c := &Client{
		ipaddr:    ipremotesocket,
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

func (s *Server) SetDeadLine(rDDL time.Duration, wDDL time.Duration) {
	s.readDDL, s.writeDDL = rDDL, wDDL
}
func (c *Client) SetDeadLine(dDDL time.Duration) {
	c.dialDDL = dDDL
}

func (s *Server) ErrChan() <-chan Errsocket {
	return s.eU
}
func (c *Client) ErrChan() <-chan Errsocket {
	return c.eU
}

func (s *Server) GetRemoteAddr() string {
	if s.currentConn == nil {
		return ""
	}
	return s.currentConn.RemoteAddr().String()
}
func (c *Client) GetRemoteAddr() string {
	if c.c == nil {
		return ""
	}
	return c.c.RemoteAddr().String()
}

func (s *Server) GetConn() net.Conn {
	return s.currentConn
}
func (c *Client) GetConn() net.Conn {
	return c.c
}

/*
will not wait for the rest of goroutines' error message.
make sure all connections has exited successfully before doing this
*/
func (s *Server) Cut() {
	s.mu.Lock()
	err := s.l.Close()
	if err != nil {
		s.eU <- Errsocket{err, s.ipaddr}
	}
	s.closed = true
	close(s.eU)
	s.mu.Unlock()
	s.cancelfunc()
}

func (c *Client) Close() { //this function will always executed(once or twice)
	c.mu.Lock()
	c.closed = true
	close(c.eU)
	c.mu.Unlock()
	c.cancelfunc()
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

func (s *Server) Listen() error { //Notifies the consumer when an error occurs ASYNCHRONOUSLY
	listener, err := net.Listen("tcp", s.ipaddr)
	if err != nil {
		return err
	}
	var ctx, cfunc = context.WithCancel(context.Background())
	s.cancelfunc = cfunc
	s.l = listener
	eC := make(chan Errsocket)
	go s.eDerfunc(eC)
	go handleListen(s, eC, ctx)
	return nil
}

func (c *Client) Dial() error {
	var (
		err        error
		ctx, cfunc = context.WithCancel(context.Background())
		eC         = make(chan Errsocket)
	)
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
	go handleConnClient(&hConnerClient{
		c:        c.c,
		d:        c.delimiter,
		readfunc: c.readfunc,
		mu:       c.mu,
	}, ctx, eC, &c.eDer, c)

	return nil
}

func handleListen(s *Server, eC chan Errsocket, ctx context.Context) {
	fmt.Println("Start hL")
	defer fmt.Println("->hL quit")
	defer close(eC)
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
			if s.readDDL != 0 {
				_ = conn.SetReadDeadline(time.Now().Add(s.readDDL))
			}
			if s.writeDDL != 0 {
				_ = conn.SetWriteDeadline(time.Now().Add(s.writeDDL))
			}

			cctx, childfunc := context.WithCancel(ctx)          //TODO: if MAXLIVETIME is needed.
			s.remoteMap[conn.RemoteAddr().String()] = childfunc //TODO: childfunc are the same???

			ceC := make(chan Errsocket)
			go s.eDerfunc(ceC)
			go handleConnServer(&hConnerServer{
				c:        conn,
				d:        s.delimiter,
				readfunc: s.readfunc,
				mu:       s.mu,
			}, cctx, ceC, s)
		}
	}
}

//what is eD: This is a patch for the Client's abnormal exit(without close()) that can turn off the error UpstreamChannel.
func handleConnServer(h *hConnerServer, ctx context.Context, eC chan Errsocket, s *Server) {
	fmt.Println("Start hCS:", h.c.LocalAddr(), "->", h.c.RemoteAddr())
	defer fmt.Println("->hCS quit", h.c.LocalAddr(), "->", h.c.RemoteAddr())
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
			h.mu.Lock() //s.mu
			s.currentConn = h.c
			err := h.readfunc(strReq, s) //requires a lock from hL
			s.currentConn = nil
			h.mu.Unlock()
			if err != nil {
				h.mu.Lock()
				eC <- Errsocket{err, h.c.RemoteAddr().String()}
			}
		}
	}
}

//what is eD: This is a patch for the Client's abnormal exit(without close()) that can turn off the error UpstreamChannel.
func handleConnClient(h *hConnerClient, ctx context.Context, eC chan Errsocket, eD *eDer, c *Client) {
	fmt.Println("Start hCC:", h.c.LocalAddr(), "->", h.c.RemoteAddr())
	defer fmt.Println("->hCC quit", h.c.LocalAddr(), "->", h.c.RemoteAddr())
	strReqChan := make(chan []byte)
	defer func() {
		if !eD.closed { //PATCH
			h.mu.Lock()
			eD.closed = true
			close(eD.eU)
			h.mu.Unlock()
		}
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
			if h.readfunc != nil {
				h.mu.Lock()                  //s.mu
				err := h.readfunc(strReq, c) //requires a lock from hL
				h.mu.Unlock()
				if err != nil {
					h.mu.Lock()
					eC <- Errsocket{err, h.c.RemoteAddr().String()}
				}
			}
		}
	}
}

/*
Server:
1. No delimiter with remoted closed connection:						DO readfunc with string read.
2. Delimiter with remoted closed connection(no delimiter found): 	EOF warning.
3. No delimiter with healthy connection:							waiting for closed.
4. Delimiter with healthy connection:								waiting for delimiter to DO readfunc
5. No delimiter with local closed: 									DO NOTHING.
6. Delimiter with local closed: 									DO NOTHING.
*/
func read(r *reader, eC chan Errsocket) {
	fmt.Println("Start read", r.c.LocalAddr(), "->", r.c.RemoteAddr())
	defer fmt.Println("->read quit", r.c.LocalAddr(), "->", r.c.RemoteAddr())
	defer func() {
		close(r.strReqChan)
	}()
	readBytes := make([]byte, 1)
	for {
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

func (t *Client) Write(i interface{}) error {
	return Write(t.c, i, t.delimiter)
}

func (t *Server) Write(i interface{}) error {
	return Write(t.currentConn, i, t.delimiter)
}

func Write(c net.Conn, i interface{}, d byte) error {
	data := Struct2byte(i)
	if d != 0 {
		data = append(data, d)
	}
	_, err := c.Write(data)
	return err
}

//---------------------------

type sliceMock struct {
	addr uintptr
	len  int
	cap  int
}

func Struct2byte(t interface{}) []byte {
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
