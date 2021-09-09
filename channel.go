package pool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// Pool implements a connection pool using buffered channels.
type Pool struct {
	size int64
	// storage for our net.Conn connections
	mu    sync.RWMutex
	conns chan net.Conn

	// net.Conn generator
	factory Factory
}

// Factory is a function to create new connections.
type Factory func() (net.Conn, error)

// NewPool returns a new pool based on buffered channels with an initial
// capacity and maximum capacity. Factory is used when initial capacity is
// greater than zero to fill the pool. A zero initialCap doesn't fill the Pool
// until a new Get() is called. During a Get(), If there is no new connection
// available in the pool, a new connection will be created via the Factory()
// method.
func NewPool(initialCap, maxCap int, factory Factory) (*Pool, error) {
	if initialCap < 0 || maxCap <= 0 || initialCap > maxCap {
		return nil, errors.New("invalid capacity settings")
	}

	p := &Pool{
		conns:   make(chan net.Conn, maxCap),
		factory: factory,
	}

	// create initial connections, if something goes wrong,
	// just close the pool error out.
	for i := 0; i < initialCap; i++ {
		conn, err := factory()
		atomic.AddInt64(&p.size, 1)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}
		p.conns <- conn
	}

	return p, nil
}

// SetCap resizes the pool.
func (p *Pool) SetCap(capacity int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns == nil {
		// check if the pool is closed
		return
	}
	oldConns := p.conns
	close(oldConns)
	p.conns = make(chan net.Conn, capacity)
	atomic.StoreInt64(&p.size, 0)
	for c := range oldConns {
		select {
		case p.conns <- c:
			atomic.AddInt64(&p.size, 1)
		default:
			return
		}
	}
}

var errNoCapacity = errors.New("no capacity")

func (p *Pool) tryNewConn() (net.Conn, error) {
	if atomic.LoadInt64(&p.size) >= int64(cap(p.conns)) {
		return nil, errNoCapacity
	}

	atomic.AddInt64(&p.size, 1)
	conn, err := p.factory()
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// Get gets a connection from the connection pool. If no connection is available
// in the pool, but the pool capacity has not been reached yet, it will be
// created with the factory. If the pool capacity has been reached and no
// connection is available, then the function will block until a connection
// is available, or the context is cancelled.
func (p *Pool) Get(ctx context.Context) (net.Conn, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.conns == nil {
		return nil, ErrClosed
	}

	// wrap our connections with out custom net.Conn implementation (wrapConn
	// method) that puts the connection back to the pool if it's closed.
	select {
	case conn := <-p.conns:
		if conn == nil {
			return nil, ErrClosed
		}

		return p.wrapConn(conn), nil
	default:
		conn, err := p.tryNewConn()
		if err == nil {
			return p.wrapConn(conn), nil
		}
		if err != errNoCapacity {
			return nil, err
		}
	}
	// if we got here, we want to block until a conn becomes available
	select {
	case conn := <-p.conns:
		if conn == nil {
			return nil, ErrClosed
		}

		return p.wrapConn(conn), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// put puts the connection back to the pool. If the pool is full or closed,
// conn is simply closed. A nil conn will be rejected.
func (p *Pool) put(conn net.Conn) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.conns == nil {
		// pool is closed, close passed connection
		return conn.Close()
	}

	// put the resource back into the pool. If the pool is full, this will
	// block and the default case will be executed.
	select {
	case p.conns <- conn:
		return nil
	default:
		// pool is full, close passed connection
		return conn.Close()
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// guard against previous closure
	if p.conns == nil {
		return
	}

	close(p.conns)
	for conn := range p.conns {
		conn.Close()
	}
	// signal to open connections that they cannot be placed back in the pool.
	p.conns = nil
	p.factory = nil
}

func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}

// newConn wraps a standard net.Conn to a poolConn net.Conn.
func (p *Pool) wrapConn(conn net.Conn) net.Conn {
	pc := &PoolConn{c: p}
	pc.Conn = conn
	return pc
}
