package register

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Register struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*ServiceImpl
}
type ServiceImpl struct {
	Addr  string //服务端的地址
	start time.Time
}

const (
	defaultPath    = "/_rpc_/registry"
	defaultTimeout = time.Minute * 5
)

func NewRegister(timeout time.Duration) *Register {
	return &Register{
		timeout: timeout,
		servers: make(map[string]*ServiceImpl),
	}
}

//给服务中心新添加俩个方法 Add 向服务中心中加入服务 Get获取服务中心的所有方法

func (r *Register) Add(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	//如果存在更新时间，否则加入
	service := r.servers[addr]
	if service == nil {
		r.servers[addr] = &ServiceImpl{Addr: addr, start: time.Now()}
	} else {
		service.start = time.Now()
	}
}

// Get 获取服务，其实是获取服务端的地址
func (r *Register) Get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var alive []string
	for addr, service := range r.servers {
		//判断是否超时
		if r.timeout == 0 || service.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	return alive
}

// ServerHTTP 使用http的形式来添加服务，获取服务
func (r *Register) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		w.Header().Set("RPC-Services", strings.Join(r.Get(), ","))
	case "POST": //除了添加服务，还可以接受心跳消息
		addr := w.Header().Get("RPC-Service")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.Add(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
func (r *Register) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
}

var DefaultRegister = NewRegister(defaultTimeout)

func HandleHTTP() {
	DefaultRegister.HandleHTTP(defaultPath)
}

// HeartBeat 通过循环定时器发送心跳消息
func HeartBeat(register, addr string, duration time.Duration) {
	if duration == 0 {
		duration = defaultTimeout - time.Duration(1)*time.Minute
	}
	var err error
	err = sendHeartInfo(register, addr)
	for err != nil {
		go func() {
			t := time.NewTicker(duration)
			<-t.C
			err = sendHeartInfo(register, addr)
		}()
	}
}

func sendHeartInfo(register, addr string) error {
	//建立一个客户端发送消息
	client := &http.Client{}
	req, err := http.NewRequest("POST", register, nil)
	if err != nil {
		log.Println("new request err", err.Error())
	}
	req.Header.Set("RPC-Service", addr)
	if _, err = client.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}
