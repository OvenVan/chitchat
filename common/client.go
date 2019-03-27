package common

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type ClientReadFunc func([]byte, ReadFuncer) error

type Client struct {
	ipaddr    string
	dialDDL   time.Duration
	delimiter byte

	readfunc ClientReadFunc

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
	mu       *sync.Mutex //lock readfunc
	readfunc ClientReadFunc
	eD       *eDer
}

func NewClient(
	ipremotesocket string,
	delim byte, readfunc ClientReadFunc, additional interface{}) *Client {

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
		additional: additional,
	}
	c.eDerfunc = errDiversion(&c.eDer)
	return c
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

func handleConnClient(h *hConnerClient, eC chan Errsocket, ctx context.Context, client *Client) {
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
				err := h.readfunc(strReq, client)
				//h.mu.Unlock()
				if err != nil {
					h.mu.Lock()
					eC <- Errsocket{err, h.conn.RemoteAddr().String()}
				}
			}
		}
	}
}

func (t *Client) Close() {
	t.mu.Lock()
	t.closed = true
	close(t.eU)
	t.mu.Unlock()
	t.cancelfunc()
}

func (t *Client) SetDeadLine(dDDL time.Duration) {
	t.dialDDL = dDDL
}

func (t *Client) ErrChan() <-chan Errsocket {
	return t.eU
}

func (t *Client) GetRemoteAddr() string {
	if t.conn == nil {
		return ""
	}
	return t.conn.RemoteAddr().String()
}
func (t *Client) GetLocalAddr() string {
	if t.conn == nil {
		return ""
	}
	return t.conn.LocalAddr().String()
}

func (t *Client) GetConn() net.Conn {
	return t.conn
}

func (t *Client) Addon() interface{} {
	return t.additional
}

func (t *Client) Write(i interface{}) error {
	return Write(t.conn, i, t.delimiter)
}
