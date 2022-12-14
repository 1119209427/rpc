package main

import (
	"fmt"
	"log"
	"net"
	client2 "rpc/client"
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

//p2p模型的演示
func startService(addr chan string) {
	//先注册服务
	var foo Foo
	if err := service.Register(&foo); err != nil {
		fmt.Println(err.Error())
	}
	//向服务传入listen
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		fmt.Println("net.Listen err", err.Error())
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	service.Accept(l)

}
func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startService(addr)
	client, _ := client2.DialTimeOut("tcp", <-addr)
	defer func() { _ = client.Close() }()
	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := Args{Num1: i, Num2: i * i}
			var reply int

			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}
