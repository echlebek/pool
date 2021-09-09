package pool

import (
	"context"
	"log"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"
)

var (
	InitialCap = 5
	MaximumCap = 30
	network    = "tcp"
	address    = "127.0.0.1:7777"
	factory    = func() (net.Conn, error) { return net.Dial(network, address) }
)

func init() {
	// used for factory function
	go simpleTCPServer()
	time.Sleep(time.Millisecond * 300) // wait until tcp server has been settled

	rand.Seed(time.Now().UTC().UnixNano())
}

func TestNew(t *testing.T) {
	_, err := newChannelPool()
	if err != nil {
		t.Errorf("New error: %s", err)
	}
}
func TestPool_Get_Impl(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Close()

	conn, err := p.Get(context.Background())
	if err != nil {
		t.Errorf("Get error: %s", err)
	}

	_, ok := conn.(*PoolConn)
	if !ok {
		t.Errorf("Conn is not of type poolConn")
	}
}

func TestPool_Get(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Close()

	_, err := p.Get(context.Background())
	if err != nil {
		t.Errorf("Get error: %s", err)
	}

	// after one get, current capacity should be lowered by one.
	if p.Len() != (InitialCap - 1) {
		t.Errorf("Get error. Expecting %d, got %d",
			(InitialCap - 1), p.Len())
	}

	// get them all
	var wg sync.WaitGroup
	for i := 0; i < (InitialCap - 1); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Get(context.Background())
			if err != nil {
				t.Errorf("Get error: %s", err)
			}
		}()
	}
	wg.Wait()

	if p.Len() != 0 {
		t.Errorf("Get error. Expecting %d, got %d",
			(InitialCap - 1), p.Len())
	}

	_, err = p.Get(context.Background())
	if err != nil {
		t.Errorf("Get error: %s", err)
	}
}

func TestPool_Put(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	// now put them all back
	for _, conn := range conns {
		conn.Close()
	}

	if p.Len() != MaximumCap {
		t.Errorf("Put error len. Expecting %d, got %d",
			MaximumCap, p.Len())
	}

	conn, _ := p.Get(context.Background())
	p.Close() // close pool

	conn.Close() // try to put into a full pool
	if p.Len() != 0 {
		t.Errorf("Put error. Closed pool shouldn't allow to put connections.")
	}
}

func TestPool_PutUnusableConn(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Close()

	// ensure pool is not empty
	conn, _ := p.Get(context.Background())
	conn.Close()

	poolSize := p.Len()
	conn, _ = p.Get(context.Background())
	conn.Close()
	if p.Len() != poolSize {
		t.Errorf("Pool size is expected to be equal to initial size")
	}

	conn, _ = p.Get(context.Background())
	if pc, ok := conn.(*PoolConn); !ok {
		t.Errorf("impossible")
	} else {
		pc.MarkUnusable()
	}
	conn.Close()
	if p.Len() != poolSize-1 {
		t.Errorf("Pool size is expected to be initial_size - 1, got %d, want %d", p.Len(), poolSize-1)
	}
}

func TestPool_UsedCapacity(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Close()

	if p.Len() != InitialCap {
		t.Errorf("InitialCap error. Expecting %d, got %d",
			InitialCap, p.Len())
	}
}

func TestPool_Close(t *testing.T) {
	p, _ := newChannelPool()

	// now close it and test all cases we are expecting.
	p.Close()

	if p.conns != nil {
		t.Errorf("Close error, conns channel should be nil")
	}

	if p.factory != nil {
		t.Errorf("Close error, factory should be nil")
	}

	_, err := p.Get(context.Background())
	if err == nil {
		t.Errorf("Close error, get conn should return an error")
	}

	if p.Len() != 0 {
		t.Errorf("Close error used capacity. Expecting 0, got %d", p.Len())
	}
}

func TestPoolGetBlockingCancelled(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.Get(ctx); err == nil {
		t.Fatal("expected non-nil error")
	} else if err != ctx.Err() {
		t.Fatal(err)
	}
}

func TestPoolGetBlocking(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	go func() {
		conns[0].Close()
	}()

	if _, err := p.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPoolConcurrent(t *testing.T) {
	p, _ := newChannelPool()
	pipe := make(chan net.Conn, 0)

	go func() {
		p.Close()
	}()

	for i := 0; i < MaximumCap; i++ {
		go func() {
			conn, _ := p.Get(context.Background())

			pipe <- conn
		}()

		go func() {
			conn := <-pipe
			if conn == nil {
				return
			}
			conn.Close()
		}()
	}
}

func TestPoolWriteRead(t *testing.T) {
	p, _ := NewPool(0, 30, factory)

	conn, _ := p.Get(context.Background())

	msg := "hello"
	_, err := conn.Write([]byte(msg))
	if err != nil {
		t.Error(err)
	}
}

func TestPoolConcurrent2(t *testing.T) {
	p, _ := NewPool(0, 30, factory)

	var wg sync.WaitGroup

	go func() {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				conn, _ := p.Get(context.Background())
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
				conn.Close()
				wg.Done()
			}(i)
		}
	}()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			conn, _ := p.Get(context.Background())
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
			conn.Close()
			wg.Done()
		}(i)
	}

	wg.Wait()
}

func TestPoolConcurrent3(t *testing.T) {
	p, _ := NewPool(0, 1, factory)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		p.Close()
		wg.Done()
	}()

	if conn, err := p.Get(context.Background()); err == nil {
		conn.Close()
	}

	wg.Wait()
}

func newChannelPool() (*Pool, error) {
	return NewPool(InitialCap, MaximumCap, factory)
}

func simpleTCPServer() {
	l, err := net.Listen(network, address)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go func() {
			buffer := make([]byte, 256)
			conn.Read(buffer)
		}()
	}
}

func TestPoolSetCap(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	p.SetCap(15)

	for i := range conns {
		conns[i].Close()
	}

	if _, err := p.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPoolSetCapFull(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	for i := range conns {
		conns[i].Close()
	}

	p.SetCap(15)

	if _, err := p.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPoolSetCapAfterClose(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	p.Close()

	// should not panic :)
	p.SetCap(10)
}

func TestPoolSetCapNoLeak(t *testing.T) {
	p, err := NewPool(0, 30, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// get/create from the pool
	conns := make([]net.Conn, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get(context.Background())
		conns[i] = conn
	}

	for i := range conns {
		conns[i].Close()
	}

	p.SetCap(15)

	for i := 15; i < MaximumCap; i++ {
		if _, err := conns[i].Read(make([]byte, 1)); err == nil {
			t.Fatalf("expected conn to be closed, got %q", err)
			err := err.(net.Error)
			if err.Timeout() {
				t.Error("error should not be a timeout")
			}
			if err.Temporary() {
				t.Error("error should not be temporary")
			}
		}
	}
}
