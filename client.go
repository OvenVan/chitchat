package chitchat

import (
	"context"
	"net"
	"sync"
	"time"
)

type ClientReadFunc func([]byte, ReadFuncer) error

type Client interface {
	Dial() error
	Close()
	SetDeadLine(time.Duration)
	ErrChan() <-chan Errsocket
	Write(interface{}) error
	GetRemoteAddr() string
	GetLocalAddr() string
}

type client struct {
	ipaddr    string
	dialDDL   time.Duration
	delimiter byte

	readfunc ClientReadFunc
	//What's rcmu: func CLOSE can be used inside readfunc, in such case, return err cannot be sent to user without rcmu.
	//Why not server: func CUT cannot be used inside readfunc.
	rcmu *sync.Mutex

	eDer
	eDerfunc eDinitfunc

	conn       net.Conn
	cancelfunc context.CancelFunc

	//tempory used for readfunc
	additional interface{}
}

type hConnerClient struct {
	conn     net.Conn
	d        byte
	rcmu     *sync.Mutex //lock for readfunc and close
	readfunc ClientReadFunc
	eD       *eDer
}

func NewClient(
	ipremotesocket string,
	delim byte, readfunc ClientReadFunc, additional interface{}) Client {

	c := &client{
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
		rcmu:       new(sync.Mutex),
		additional: additional,
	}
	c.eDerfunc = errDiversion(&c.eDer)
	return c
}

func (t *client) Dial() error {
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
		rcmu:     t.rcmu,
		//mu:       t.mu,
		eD: &t.eDer,
	}, eC, ctx, t)

	return nil
}

func handleConnClient(h *hConnerClient, eC chan Errsocket, ctx context.Context, client *client) {
	//fmt.Println("Start hCC:", h.conn.LocalAddr(), "->", h.conn.RemoteAddr())
	//defer fmt.Println("->hCC quit", h.conn.LocalAddr(), "->", h.conn.RemoteAddr())
	strReqChan := make(chan []byte)
	defer func() {
		if !h.eD.closed {
			h.eD.mu.Lock()
			h.eD.closed = true
			close(h.eD.eU)
			h.eD.mu.Lock()
		}
		err := h.conn.Close()
		<-strReqChan
		if err != nil {
			h.eD.mu.Lock()
			eC <- Errsocket{err, h.conn.RemoteAddr().String()}
		}
		close(eC)
	}()

	go read(&reader{
		conn:       h.conn,
		d:          h.d,
		mu:         h.eD.mu,
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
				h.rcmu.Lock()
				err := h.readfunc(strReq, client)
				if err != nil {
					h.eD.mu.Lock()
					eC <- Errsocket{err, h.conn.RemoteAddr().String()}
				}
				h.rcmu.Unlock()
			}
		}
	}
}

func (t *client) Close() {
	go func() {
		t.rcmu.Lock()
		t.mu.Lock()
		t.closed = true
		close(t.eU)
		t.mu.Unlock()
		t.rcmu.Unlock()
		t.cancelfunc()
	}()
}

func (t *client) SetDeadLine(dDDL time.Duration) {
	t.dialDDL = dDDL
}

func (t *client) ErrChan() <-chan Errsocket {
	return t.eU
}

func (t *client) GetRemoteAddr() string {
	if t.conn == nil {
		return ""
	}
	return t.conn.RemoteAddr().String()
}
func (t *client) GetLocalAddr() string {
	if t.conn == nil {
		return ""
	}
	return t.conn.LocalAddr().String()
}

func (t *client) GetConn() net.Conn {
	return t.conn
}

func (t *client) Addon() interface{} {
	return t.additional
}

func (t *client) Write(i interface{}) error {
	return writeFunc(t.conn, i, t.delimiter)
}
