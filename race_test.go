package pool

import (
	"context"
	"net"
	"sync"
	"testing"
)

func TestPoolRace(t *testing.T) {
	for i := 0; i < 1000 && !t.Failed(); i++ {
		testPoolRace(t)
	}
}

func testPoolRace(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	p, err := NewPool(0, 10, func() (net.Conn, error) { return nopCloser{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := make(chan struct{})
	end := make(chan net.Conn, 20)

	for j := 0; j < cap(end); j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conn, _ := p.Get(ctx)
			end <- conn
		}()
	}

	close(start)
	var conns []net.Conn

	for len(end) < 10 {
	}
capturing:
	for k := 0; k < cap(end); k++ {
		select {
		default:
			break capturing
		case conn := <-end:
			conns = append(conns, conn)
		}
	}

	if len(conns) != 10 {
		t.Error("expected", 10, "but got ", len(conns))
	}
}

type nopCloser struct{ net.Conn }

func (nopCloser) Close() error { return nil }
