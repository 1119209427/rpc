package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"rpc/service"
	"strings"
)

//对传入的conn加上connect方法字段
//但是不用对opt进行编码，不用集合接受信息的方法，只用把这个conn加上connect发给服务端即可
//通过 HTTP CONNECT 请求建立连接之后，后续的通信过程就交给 NewClient 了。
const (
	connected        = "200 Connected to RPC"
	defaultRPCPath   = "/_prc_"
	defaultDebugPath = "/debug/rpc"
)

func NewHTTPClient(conn net.Conn, opt *service.Option) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))
	//选择pro格式前需要确定服务端回应
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err

}
func DialHTTP(network, address string, opts ...*service.Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// rpcAddr  eg, http@10.0.0.1:7001, tcp@10.0.0.1:9999, unix@/tmp/rpc.sock

func XDial(rpcAddr string, opts ...*service.Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	//根据part[1]调用dial方法

	switch parts[0] {
	case "http":
		return DialHTTP(parts[0], parts[1], opts...)
	default:
		return DialTimeOut(parts[0], parts[1], opts...)
	}

}
