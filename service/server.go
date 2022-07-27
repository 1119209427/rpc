package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"io"
	"log"
	"net"
	"reflect"
	"rpc/coder"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// func (t *T) MethodName(argType T1, replyType *T2) error
//满足这种形式才可以满足rpc调用

type methodType struct {
	method    reflect.Method
	ArgvType  reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

//给入参实例化
func (m *methodType) newArg() reflect.Value {
	//指针类型的实例化
	var argv reflect.Value
	if m.ArgvType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgvType.Elem())
	} else {
		//reflect.New 新建一个指针对象
		argv = reflect.New(m.ArgvType).Elem()
	}

	return argv
}

func (m *methodType) newReply() reflect.Value {
	//第二个入参必须是指针类型
	reply := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		reply.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		reply.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return reply
}

//单独的服务
type server struct {
	name   string                 //服务的名字
	typ    reflect.Type           //服务的类型
	rcvr   reflect.Value          //这个服务本身，需要在rcvr作为第一个参数
	method map[string]*methodType //储存所有满足rpc条件的方法
}

func newServer(rcvr interface{}) *server {
	s := new(server)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name() //如果传入指针类型，想获取名字只能用这种方法
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) { //报告 id 是否以大写字母开头。
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	//注册服务
	s.registerMethod()
	return s
}
func (server *server) registerMethod() {
	//先make map
	server.method = make(map[string]*methodType)
	for i := 0; i < server.typ.NumMethod(); i++ {
		method := server.typ.Method(i)
		//判断这个方法是否符合rpc的通信要求
		methodT := method.Type
		//入参包含自身，应该有三个
		if methodT.NumIn() != 3 && methodT.NumOut() != 1 {
			//不注册，跳过
			continue
		}
		//判断出参是否为err
		if methodT.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		//判断入参(名字是否大写，包的路径是否正确)
		argType, replyType := methodT.In(1), methodT.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		server.method[method.Name] = &methodType{
			method:    method,
			ArgvType:  argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", server.name, method.Name)
	}
}

//通过反射值调用方法
//处理请求时调用
func (server *server) call(method *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&method.numCalls, 1)
	f := method.method.Func
	returnValues := f.Call([]reflect.Value{server.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

const MagicNumber = 0x3bef5c

// Option 消息的编解码方式
type Option struct {
	MagicNumber    int //MagicNumber 标记这是一个 rpc 请求
	Type           coder.Type
	ConnectTimeout time.Duration // 0 表示没有超时限制
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	Type:           coder.GobType,
	ConnectTimeout: time.Second * 10,
}

//为了简单RPC 客户端固定采用 JSON 编码 Option，后续的 header 和 body 的编码方式由 Option 中的 CodeType 指定，
//服务端首先使用 JSON 解码 Option，然后通过 Option 的 CodeType 解码剩余的内容。即报文将以这样的形式发送：
/*| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|*/
// 后续可以实现自定义编码

// Server 实现服务端
type Server struct {
	serverMap sync.Map
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Register(rcvr interface{}) error {
	service := newServer(rcvr)
	//去map中是否有这个服务,没有则注册
	if _, ok := s.serverMap.LoadOrStore(service.name, service); ok {
		return errors.New("rpc: service already defined: " + service.name)
	}
	return nil
}

//读取请求时使用
func (s *Server) findServer(ServiceMethod string) (svc *server, mtype *methodType, err error) {
	dot := strings.LastIndex(ServiceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + ServiceMethod)
		return
	}
	serviceName, methodName := ServiceMethod[:dot], ServiceMethod[dot+1:]
	svci, ok := s.serverMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: service not find: ")
	}
	svc = svci.(*server)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

// Accept 接受客户端的conn，并处理
func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("监听失败", err)
			return
		}
		//起一个go程去处理这个conn
		go s.ServerConn(conn)
	}
}

var DefaultServer = NewServer()

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}
func Register(rcvr interface{}) error { return DefaultServer.Register(rcvr) }

// ServerConn 处理过程分为 处理请求，回复请求
//而处理请求是并发执行的，但是回复请求要一条一条来，防止沾包的现象出现
func (s *Server) ServerConn(conn io.ReadWriteCloser) {
	//根据通信协议先反序列化option
	defer func() { conn.Close() }()
	decoder := json.NewDecoder(conn)
	var opt Option
	if err := decoder.Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
	}
	if opt.MagicNumber != MagicNumber {
		log.Println("rpc server: MagicNumber error:", opt.MagicNumber)
	}
	//通过传进来的类型获取coder,进行对请求头和请求体的反序列化
	coder := coder.NewCoderMap[opt.Type]
	if coder == nil {
		log.Println("rpc server: type error:", opt.Type)
	}
	if opt.HandleTimeout == 0 {
		opt.HandleTimeout = DefaultOption.ConnectTimeout
	}
	s.ServerCoder(coder(conn), opt.HandleTimeout)
}

type request struct {
	h            *coder.Header // header of request
	argv, replyv reflect.Value // argv and replyv of request
	mType        *methodType
	server       *server
}

//invalidRequest 是发生错误时响应 argv 的占位符
var invalidRequest = struct{}{}

func (s *Server) ServerCoder(c coder.Codec, timeout time.Duration) {
	//因为并发，所以要给这个方法用wg等待处理请求的并发
	sending := new(sync.Mutex) //一条一条回复，上锁
	wg := new(sync.WaitGroup)
	for {
		req, err := s.readRequest(c)
		if err != nil {
			if req == nil {
				break //退出接受请求，这种情况是不可能的
			}
			//将错误消息写入
			req.h.Err = err.Error()
			//将错误消息写回客户端
			s.sendingResponse(c, req.h, invalidRequest, sending)
			continue
		}
		//否则就处理请求
		wg.Add(1)
		go s.handelRequest(c, req, wg, sending, timeout)

	}
	wg.Wait()
	_ = c.Close()
}

func (s *Server) handelRequest(c coder.Codec, req *request, wg *sync.WaitGroup, sending *sync.Mutex, timeout time.Duration) {
	defer func() {
		wg.Done()
	}()
	//处理请求,调用方法
	called := make(chan struct{}) //调用call后传输消息的管道
	sended := make(chan struct{})
	go func() {
		err := req.server.call(req.mType, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Err = err.Error()
			s.sendingResponse(c, req.h, invalidRequest, sending)
			sended <- struct{}{}
			return
		}
		s.sendingResponse(c, req.h, req.replyv.Interface(), sending)
		sended <- struct{}{}
	}()
	select {
	case <-time.After(timeout):
		req.h.Err = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		s.sendingResponse(c, req.h, invalidRequest, sending)
	//called 信道接收到消息，代表处理没有超时，继续执行 sendResponse。
	case <-called:
		<-sended
	}

}
func (s *Server) readRequest(c coder.Codec) (*request, error) {
	h, err := s.readRequestHeader(c)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	//需要先判断argv的类型
	req.server, req.mType, err = s.findServer(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	//确保arv为指针类型
	req.argv = req.mType.newArg()
	req.replyv = req.mType.newReply()
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = c.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
	}
	return req, nil

}
func (s *Server) readRequestHeader(c coder.Codec) (*coder.Header, error) {
	var h coder.Header
	err := c.ReaderHeader(&h)
	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF { //两个错误都是读取后成功后报的错
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}
func (s *Server) sendingResponse(c coder.Codec, header *coder.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	err := c.Write(header, body)
	if err != nil {
		log.Println("rpc server: write header body error", err)
	}
}
