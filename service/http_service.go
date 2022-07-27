package service

import (
	"io"
	"log"
	"net/http"
)

const (
	connected        = "200 Connected to RPC"
	defaultRPCPath   = "/_prc_"
	defaultDebugPath = "/debug/rpc"
)

/*RPC 服务端来，需要做的是将 HTTP 协议转换为 RPC 协议，
对客户端来说，需要新增通过 HTTP CONNECT 请求创建连接的逻辑。
*/

// ServerHttp 为了实现 HttpHandel接口，实现改接口就可以http通信了
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	//判断请求的方法是不是connect
	if req.Method != "CONNECT" {
		//将错误消息写回resp
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}
	//如果正确则将http 转化为rpc
	//获取一个http conn
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	//将收到conn方法回复写回，不直接写会w原因是，要通过自定义的rpc通信
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	//抛给service中方法处理conn
	s.ServerConn(conn)
}

// HandleHTTP 现在服务端可以处理http请求了
func (s *Server) HandleHTTP() {
	http.Handle(defaultDebugPath, s)
}

// HandleHTTP 方便外部调用
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}
