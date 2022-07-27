package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"rpc/register"
	"rpc/service"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}
func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}
func startService(addr chan string) {
	//注册方法
	var foo Foo
	if err := service.Register(foo); err != nil {
		fmt.Println(err.Error())
	}
	//启动监听
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		fmt.Println("net.Listen err", err.Error())
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	service.Accept(l)
}
func foo(xc *register.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var reply int
	var err error
	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	}
	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}
func call(addr1, addr2 string) {
	//注册服务中心
	d := register.NewMultiServersDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
	//注册服务中心的客户端
	xc := register.NewXClient(d, nil, register.RoundRobinSelect)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i * i})
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			foo(xc, ctx, "call", "Foo.Sleep", &Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}
func main() {
	log.SetFlags(0)
	ch1 := make(chan string)
	ch2 := make(chan string)
	// start two servers
	go startService(ch1)
	go startService(ch2)

	addr1 := <-ch1
	addr2 := <-ch2

	time.Sleep(time.Second)
	call(addr1, addr2)

}
