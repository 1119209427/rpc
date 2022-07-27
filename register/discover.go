package register

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type Discovery struct {
	*MultiServersDiscovery               //负载均衡
	register              string        //注册中心地址
	timeout               time.Duration //服务列表的过期时间
	lastUpdate            time.Time     //最后在服务中心更新服务列表的时间
}

const defaultUpdateTimeout = time.Second * 10

func NewGeeRegistryDiscovery(registerAddr string, timeout time.Duration) *Discovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &Discovery{
		MultiServersDiscovery: NewMultiServersDiscovery(make([]string, 0)),
		register:              registerAddr,
		timeout:               timeout,
	}
	return d
}
func (d *Discovery) Update(service []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = service
	d.lastUpdate = time.Now()
	return nil
}
func (d *Discovery) Refresh() error {
	//判断服务是否过期，不过期不用更新
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil //不用跟新
	}
	log.Println("rpc registry: refresh servers from registry", d.register)
	//获取服务列表
	resp, err := http.Get(d.register)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}
	servers := strings.Split(resp.Header.Get("RPC-Services"), ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}
func (d *Discovery) Get(model SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(model)
}
func (d *Discovery) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll()
}
