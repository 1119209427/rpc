package register

import (
	"context"
	"errors"
	"io"
	"rpc/client"
	"rpc/service"
	"sync"
)

type XClient struct {
	d       Discover
	Opt     *service.Option
	mode    SelectMode
	mu      sync.Mutex
	clients map[string]*client.Client
}

var _ io.Closer = (*XClient)(nil)

func NewXClient(d Discover, opt *service.Option, mode SelectMode) *XClient {
	return &XClient{
		d:       d,
		Opt:     opt,
		mode:    mode,
		clients: make(map[string]*client.Client),
	}
}
func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}
func (xc *XClient) dial(rpcAddr string) (*client.Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	c, ok := xc.clients[rpcAddr]
	if ok && !c.IsAvailable() {
		_ = c.Close()
		delete(xc.clients, rpcAddr)
	}
	if c == nil {
		var err error
		c, err = client.XDial(rpcAddr, xc.Opt)
		if err != nil {
			err = errors.New("rpc register dial error")
			return nil, err
		}
		xc.clients[rpcAddr] = c
	}
	return c, nil
}
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	c, err := xc.dial(rpcAddr)
	if err != nil {
		return errors.New("rpc register dial error")
	}
	err = c.Call2(ctx, serviceMethod, args, reply)
	if err != nil {
		return err
	}
	return nil
}
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}
