package common

import (
	"bytes"
	"context"
	"errors"
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

	//remoteMap map[string]context.CancelFunc
	remoteMap *sync.Map

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

	conn       net.Conn
	cancelfunc context.CancelFunc
}

type hConnerServer struct {
	conn     net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc ServerReadFunc
}

type hConnerClient struct {
	conn     net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc ClientReadFunc
	eD       *eDer
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
		//remoteMap: make(map[string]context.CancelFunc),
		remoteMap: new(sync.Map),
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

func (t *Server) SetDeadLine(rDDL time.Duration, wDDL time.Duration) {
	t.readDDL, t.writeDDL = rDDL, wDDL
}
func (t *Client) SetDeadLine(dDDL time.Duration) {
	t.dialDDL = dDDL
}

func (t *Server) ErrChan() <-chan Errsocket {
	return t.eU
}
func (t *Client) ErrChan() <-chan Errsocket {
	return t.eU
}

func (t *Server) GetRemoteAddr() string {
	if t.currentConn == nil {
		return ""
	}
	return t.currentConn.RemoteAddr().String()
}
func (t *Client) GetRemoteAddr() string {
	if t.conn == nil {
		return ""
	}
	return t.conn.RemoteAddr().String()
}

func (t *Server) GetConn() net.Conn {
	return t.currentConn
}
func (t *Client) GetConn() net.Conn {
	return t.conn
}

/*
will not wait for the rest of goroutines' error message.
make sure all connections has exited successfully before doing this
*/
func (t *Server) Cut() {
	t.mu.Lock()
	err := t.l.Close()
	if err != nil {
		t.eU <- Errsocket{err, t.ipaddr}
	}
	t.closed = true
	close(t.eU)
	t.mu.Unlock()
	t.cancelfunc()
}

func (t *Server) Close(remoteAddr string) {
	//t.remoteMap[remoteAddr]()
	x, ok := t.remoteMap.Load(remoteAddr)
	if !ok {
		t.eU <- Errsocket{errors.New(remoteAddr + " does not connected to this server"), t.ipaddr}
		return
	}
	x.(context.CancelFunc)()
	t.remoteMap.Delete(remoteAddr)
}

func (t *Client) Close() {
	t.mu.Lock()
	t.closed = true
	close(t.eU)
	t.mu.Unlock()
	t.cancelfunc()
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

func (t *Server) Listen() error { //Notifies the consumer when an error occurs ASYNCHRONOUSLY
	if t.readfunc == nil {
		return errors.New("read function is nil")
	}
	listener, err := net.Listen("tcp", t.ipaddr)
	if err != nil {
		return err
	}

	var ctx, cfunc = context.WithCancel(context.Background())
	t.cancelfunc = cfunc
	t.l = listener
	eC := make(chan Errsocket)
	go t.eDerfunc(eC)
	go handleListen(t, eC, ctx)
	return nil
}

func (t *Client) Dial() error {
	var (
		err        error
		ctx, cfunc = context.WithCancel(context.Background())
		eC         = make(chan Errsocket)
	)
	t.cancelfunc = cfunc

	if t.dialDDL == 0 {
		t.conn, err = net.Dial("tcp", t.ipaddr)
	} else {
		t.conn, err = net.DialTimeout("tcp", t.ipaddr, t.dialDDL)
	}
	if err != nil {
		return err
	}
	go t.eDerfunc(eC)
	go handleConnClient(&hConnerClient{
		conn:     t.conn,
		d:        t.delimiter,
		readfunc: t.readfunc,
		mu:       t.mu,
		eD:       &t.eDer,
	}, eC, ctx, t)

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

			cctx, childfunc := context.WithCancel(ctx) //TODO: if MAXLIVETIME is needed.
			//s.remoteMap[conn.RemoteAddr().String()] = childfunc
			s.remoteMap.Store(conn.RemoteAddr().String(), childfunc)

			ceC := make(chan Errsocket)
			go s.eDerfunc(ceC)
			go handleConnServer(&hConnerServer{
				conn:     conn,
				d:        s.delimiter,
				readfunc: s.readfunc,
				mu:       s.mu,
			}, ceC, cctx, s)
		}
	}
}

func handleConnServer(h *hConnerServer, eC chan Errsocket, ctx context.Context, s *Server) {
	fmt.Println("Start hCS:", h.conn.LocalAddr(), "->", h.conn.RemoteAddr())
	defer fmt.Println("->hCS quit", h.conn.LocalAddr(), "->", h.conn.RemoteAddr())
	strReqChan := make(chan []byte)
	defer func() {
		err := h.conn.Close()
		<-strReqChan
		if err != nil {
			h.mu.Lock()
			eC <- Errsocket{err, h.conn.RemoteAddr().String()}
		}
		close(eC)
	}()

	go read(&reader{
		conn:       h.conn,
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
			//h.mu.Lock() //s.mu		why does it need a lock?? Temporary removed it.
			err := h.readfunc(strReq, &Server{
				currentConn: h.conn,
				delimiter:   h.d,
				//remoteMap:   s.remoteMap,
				remoteMap: s.remoteMap,
			}) //requires a lock from hL
			//h.mu.Unlock()
			if err != nil {
				h.mu.Lock()
				eC <- Errsocket{err, h.conn.RemoteAddr().String()}
			}
		}
	}
}

func handleConnClient(h *hConnerClient, eC chan Errsocket, ctx context.Context, c *Client) {
	fmt.Println("Start hCC:", h.conn.LocalAddr(), "->", h.conn.RemoteAddr())
	defer fmt.Println("->hCC quit", h.conn.LocalAddr(), "->", h.conn.RemoteAddr())
	strReqChan := make(chan []byte)
	defer func() {
		if !h.eD.closed {
			h.mu.Lock()
			h.eD.closed = true
			close(h.eD.eU)
			h.mu.Unlock()
		}
		err := h.conn.Close()
		<-strReqChan
		if err != nil {
			h.mu.Lock()
			eC <- Errsocket{err, h.conn.RemoteAddr().String()}
		}
		close(eC)
	}()

	go read(&reader{
		conn:       h.conn,
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
				//h.mu.Lock()                  //s.mu
				err := h.readfunc(strReq, c) //requires a lock from hL
				//h.mu.Unlock()
				if err != nil {
					h.mu.Lock()
					eC <- Errsocket{err, h.conn.RemoteAddr().String()}
				}
			}
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
				fmt.Println(err.Error())
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

func (t *Client) Write(i interface{}) error {
	return Write(t.conn, i, t.delimiter)
}
func (t *Server) Write(i interface{}) error {
	return Write(t.currentConn, i, t.delimiter)
}
func Write(c net.Conn, i interface{}, d byte) error {
	data := struct2byte(i)
	if d != 0 {
		data = append(data, d)
	}
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
