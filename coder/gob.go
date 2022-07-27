package coder

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser
	//编码
	enc *gob.Encoder
	//解码
	dec *gob.Decoder
	//buf 是为了防止阻塞而创建的带缓冲的 Writer
	buf *bufio.Writer
}

//编译时，不用检测是否有结构体实现了接口
var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

//去实现coder的接口

// ReaderHeader  实现解码过程，反序列化
func (g *GobCodec) ReaderHeader(header *Header) error {
	return g.dec.Decode(header)
}
func (g *GobCodec) ReadBody(body interface{}) error {
	return g.dec.Decode(body)
}
func (g *GobCodec) Write(header *Header, body interface{}) error {
	defer func() {
		err := g.buf.Flush()
		if err != nil {
			_ = g.Close()
		}
	}()

	if err := g.enc.Encode(header); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}
	if err := g.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}
func (g *GobCodec) Close() error {
	return g.conn.Close()
}
