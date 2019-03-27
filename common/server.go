package common

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type ServerReadFunc func([]byte, ReadFuncer) error

type Server struct {
	//unchangable data
	ipaddr   string
	readDDL  time.Duration
	writeDDL time.Duration
	//if delimiter is 0, then read until it's EOF
	delimiter byte
	readfunc  ServerReadFunc

	remoteMap *sync.Map

	//under protected data
	eDer
	eDerfunc eDinitfunc

	l          net.Listener
	cancelfunc context.CancelFunc //cancelfunc for cancel listener(CUT)

	//tempory used for readfunc
	currentConn net.Conn
	additional  interface{}
}

type hConnerServer struct {
	conn     net.Conn
	d        byte
	mu       *sync.Mutex //lock readfunc
	readfunc ServerReadFunc
}

func NewServer(
	ipaddrsocket string,
	delim byte, readfunc ServerReadFunc, additional interface{}) *Server {

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
		remoteMap:  new(sync.Map),
		readfunc:   readfunc,
		additional: additional,
	}
	s.eDerfunc = errDiversion(&s.eDer)
	return s
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
				remoteMap:   s.remoteMap,
				additional:  s.additional,
			}) //requires a lock from hL
			//h.mu.Unlock()
			if err != nil {
				h.mu.Lock()
				eC <- Errsocket{err, h.conn.RemoteAddr().String()}
			}
		}
	}
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

func (t *Server) CloseRemote(remoteAddr string) {
	x, ok := t.remoteMap.Load(remoteAddr)
	if !ok {
		t.eU <- Errsocket{errors.New(remoteAddr + " does not connected to this server"), t.ipaddr}
		return
	}
	x.(context.CancelFunc)()
	t.remoteMap.Delete(remoteAddr)
}

func (t *Server) Close() {
	remoteAddr := t.currentConn.RemoteAddr().String()
	x, ok := t.remoteMap.Load(remoteAddr)
	if !ok {
		t.eU <- Errsocket{errors.New("internal error"), t.ipaddr}
		return
	}
	x.(context.CancelFunc)()
	t.remoteMap.Delete(remoteAddr)
}

func (t *Server) RangeConn() []string {
	rtnstring := make([]string, 0)
	t.remoteMap.Range(
		func(key, value interface{}) bool {
			rtnstring = append(rtnstring, key.(string))
			return true
		})
	return rtnstring
}

func (t *Server) SetDeadLine(rDDL time.Duration, wDDL time.Duration) {
	t.readDDL, t.writeDDL = rDDL, wDDL
}

func (t *Server) ErrChan() <-chan Errsocket {
	return t.eU
}

func (t *Server) GetRemoteAddr() string {
	if t.currentConn == nil {
		return ""
	}
	return t.currentConn.RemoteAddr().String()
}

func (t *Server) GetLocalAddr() string {
	if t.currentConn == nil {
		return ""
	}
	return t.currentConn.LocalAddr().String()
}

func (t *Server) GetConn() net.Conn {
	return t.currentConn
}

func (t *Server) Addon() interface{} {
	return t.additional
}

func (t *Server) Write(i interface{}) error {
	return Write(t.currentConn, i, t.delimiter)
}
