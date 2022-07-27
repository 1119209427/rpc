package register

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// SelectMode 负载平衡模式的选择
type SelectMode int

const (
	RandomSelect     SelectMode = iota // select randomly
	RoundRobinSelect                   // select using Robbin algorithm
)

type Discover interface {
	// Refresh 刷新服务
	Refresh() error
	// Update 手动刷新服务
	Update(service []string) error
	// Get 获取服务
	Get(model SelectMode) (string, error)
	// GetAll 获取所有服务
	GetAll() ([]string, error)
}

type MultiServersDiscovery struct {
	r       *rand.Rand
	mu      sync.RWMutex
	servers []string
	index   int //记录是哪一个服务
}

func NewMultiServersDiscovery(servers []string) *MultiServersDiscovery {
	d := &MultiServersDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

//实现discover接口
var _Discover = (*MultiServersDiscovery)(nil)

func (m *MultiServersDiscovery) Refresh() error {
	return nil
}
func (m *MultiServersDiscovery) Update(service []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = service
	return nil
}
func (m *MultiServersDiscovery) Get(model SelectMode) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.servers)
	if n == 0 {
		return "", errors.New("rpc register: no service find")
	}
	switch model {
	case RandomSelect:
		return m.servers[m.r.Intn(n)], nil
	case RoundRobinSelect:
		s := m.servers[m.index%n]
		m.index = (m.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc register : no select mode")
	}
}

// GetAll 返回服务切片的副本，防止被修改
func (m *MultiServersDiscovery) GetAll() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	servers := make([]string, len(m.servers), cap(m.servers))
	copy(servers, m.servers)
	return servers, nil
}
