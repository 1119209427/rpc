package coder

import "io"

//使用 encoding/gob 实现消息的编解码(序列化与反序列化)

//客户端发送的请求包括服务名 Arith，方法名 Multiply，参数 args 三个，服务端的响应包括错误 error，返回值 reply 2 个。
//我们将请求和响应中的参数和返回值抽象为 body，剩余的信息放在 header 中，那么就可以抽象出数据结构 Header：

type Header struct {
	ServiceMethod string // UserLogin.Login
	Seq           uint64 //客户端发送请求的编号，用来区分
	Err           string
}

// Codec 对消息进行编解码，包括客户端和服务端，所以抽象成借口
type Codec interface {
	io.Closer
	// ReaderHeader 读取消息头的方法
	ReaderHeader(*Header) error
	// ReadBody 读取body的方法
	ReadBody(interface{}) error
	// Write 写回请求
	Write(*Header, interface{}) error
}

type NewCoderFunc func(io.ReadWriteCloser) Codec
type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // not implemented
)

var NewCoderMap map[Type]NewCoderFunc

func init() {
	NewCoderMap = make(map[Type]NewCoderFunc)
	NewCoderMap[GobType] = NewGobCodec
}
