package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"rpc/coder"
	"rpc/service"
	"sync"
	"time"
)

// Call 封装一次rpc中所有信息
type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call //为了实现异步调用
}

func (c *Call) done() {
	c.Done <- c
}

type Client struct {
	cc       coder.Codec //编码，解码
	sending  sync.Mutex  //发送请求，防止粘包，上锁，一条一条发送
	opt      *service.Option
	mu       sync.Mutex
	seq      uint64 //seq 用于给发送的请求编号，每个请求拥有唯一编号。
	heard    coder.Header
	pending  map[uint64]*Call //用来存储还未执行请求
	closing  bool             //主动关闭
	shutDown bool             //出现错误时关闭
}

var ErrShutdown = errors.New("connection is shut down")

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return ErrShutdown
	}
	c.closing = true
	return c.cc.Close()
}
func (c *Client) IsAvailable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.shutDown && !c.closing
}

//注册一个call，返回这个call的编号
func (c *Client) registerCall(call *Call) (uint64, error) {
	c.mu.Lock()
	defer func() {
		c.mu.Unlock()
	}()
	if c.shutDown || c.closing {
		return 0, ErrShutdown
	}
	call.Seq = c.seq

	//c.pending = make(map[uint64]*Call)
	c.pending[call.Seq] = call
	c.seq++
	return call.Seq, nil
}

//从pending删除这个call，并返回
func (c *Client) removeCall(seq uint64) *Call {
	c.mu.Lock()
	defer func() {
		c.mu.Unlock()
	}()
	if call, ok := c.pending[seq]; ok {
		delete(c.pending, seq)
		return call
	}
	return nil
}

//服务端或客户端发生错误时调用，将 shutdown 设置为 true，且将错误信息通知所有 pending 状态的 call。
func (c *Client) terminateCalls(err error) {
	c.sending.Lock()
	defer c.sending.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closing, c.shutDown = true, true
	for _, call := range c.pending {
		call.Error = err
		call.done()
	}
}

//实现客户端最重要的功能，发起请求，和接受回复
func (c *Client) receiver() {
	//三种情况
	//一种，call为空
	//第二种。call不为空，但是有错误
	//第三种，接收到没有错误的call
	var err error
	for err == nil {
		var h coder.Header
		if err = c.cc.ReaderHeader(&h); err != nil {
			break
		}
		//从头消息中获取到seq,获取到call
		call := c.removeCall(h.Seq)
		switch {
		case call == nil:
			err = c.cc.ReadBody(nil)
		case h.Err != "":
			call.Error = fmt.Errorf(h.Err)
			err = c.cc.ReadBody(nil)
			call.done()
		default:
			err = c.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body" + err.Error())
			}
			call.done()
		}
	}
	c.terminateCalls(err)

}

// NewClient 在初始化服务器的同时接收消息
func NewClient(conn net.Conn, opt *service.Option) (*Client, error) {
	f := coder.NewCoderMap[opt.Type]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.Type)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	//将opt消息发送发给服务端
	encoder := json.NewEncoder(conn)
	err := encoder.Encode(opt)
	if err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCoder(f(conn), opt), nil

}
func newClientCoder(cc coder.Codec, opt *service.Option) *Client {
	client := &Client{
		cc:      cc,
		opt:     opt,
		seq:     1,
		pending: make(map[uint64]*Call),
	}
	go client.receiver()
	return client
}
func parseOptions(opts ...*service.Option) (*service.Option, error) {
	// if opts is nil or pass nil as parameter
	if len(opts) == 0 || opts[0] == nil {
		return service.DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = service.DefaultOption.MagicNumber
	if opt.Type == "" {
		opt.Type = service.DefaultOption.Type
	}
	return opt, nil
}
func Dial(network, address string, opts ...*service.Option) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		log.Println("dial err", err.Error())
		return nil, err
	}
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	client, err := NewClient(conn, opt)
	if err != nil {
		return nil, err
	}
	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	return client, nil
}

type clientResult struct {
	client *Client
	err    error
}
type newClientFunc func(net.Conn, *service.Option) (*Client, error)

// DialTimeOut 超时处理的dial
func DialTimeOut(network, address string, opts ...*service.Option) (*Client, error) {

	return dialTimeout(NewClient, network, address, opts...)

}
func dialTimeout(f newClientFunc, network, address string, opts ...*service.Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		log.Println("dial err", err.Error())
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	send := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		if err != nil {
			_ = conn.Close()
		}
		send <- clientResult{client: client, err: err}
	}()
	if opt.ConnectTimeout == 0 {
		result := <-send
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectTimeout):
		//说明超时了
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-send:
		return result.client, result.err
	}

}

//发起请求
func (c *Client) send(call *Call) {
	c.sending.Lock()
	defer c.sending.Unlock()
	seq, err := c.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	//准备消息头
	c.heard.ServiceMethod = call.ServiceMethod
	c.heard.Seq = seq
	c.heard.Err = ""
	//编码并发送request
	err = c.cc.Write(&c.heard, call.Args)
	if err != nil {
		call := c.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go 异步发送消息 通过将call传回告诉客户端完成了发送信息
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	c.send(call)
	return call
}

// Call 同步发送消息
func (c *Client) Call(serviceMethod string, args, reply interface{}) error {
	call := <-c.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}

// Call2 用户自定义超时处理
func (c *Client) Call2(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := c.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case call := <-call.Done:
		return call.Error
	case <-ctx.Done():
		c.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	}

}
