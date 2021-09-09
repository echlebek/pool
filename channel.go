package pool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
)

// Pool implements a connection pool using buffered channels.
type Pool struct {
	size int
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

	c := &Pool{
		conns:   make(chan net.Conn, maxCap),
		factory: factory,
	}

	// create initial connections, if something goes wrong,
	// just close the pool error out.
	for i := 0; i < initialCap; i++ {
		conn, err := factory()
		c.size++
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}
		c.conns <- conn
	}

	return c, nil
}

func (c *Pool) getConnsAndFactory() (chan net.Conn, Factory) {
	c.mu.RLock()
	conns := c.conns
	factory := c.factory
	c.mu.RUnlock()
	return conns, factory
}

var errNoCapacity = errors.New("no capacity")

func (c *Pool) tryNewConn() (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.size >= cap(c.conns) {
		return nil, errNoCapacity
	}

	c.size++
	conn, err := c.factory()
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
func (c *Pool) Get(ctx context.Context) (net.Conn, error) {
	conns, _ := c.getConnsAndFactory()
	if conns == nil {
		return nil, ErrClosed
	}

	// wrap our connections with out custom net.Conn implementation (wrapConn
	// method) that puts the connection back to the pool if it's closed.
	select {
	case conn := <-conns:
		if conn == nil {
			return nil, ErrClosed
		}

		return c.wrapConn(conn), nil
	default:
		conn, err := c.tryNewConn()
		if err == nil {
			return c.wrapConn(conn), nil
		}
		if err != errNoCapacity {
			return nil, err
		}
	}
	// if we got here, we want to block until a conn becomes available
	select {
	case conn := <-conns:
		if conn == nil {
			return nil, ErrClosed
		}

		return c.wrapConn(conn), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// put puts the connection back to the pool. If the pool is full or closed,
// conn is simply closed. A nil conn will be rejected.
func (c *Pool) put(conn net.Conn) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conns == nil {
		// pool is closed, close passed connection
		return conn.Close()
	}

	// put the resource back into the pool. If the pool is full, this will
	// block and the default case will be executed.
	select {
	case c.conns <- conn:
		return nil
	default:
		// pool is full, close passed connection
		return conn.Close()
	}
}

func (c *Pool) Close() {
	c.mu.Lock()
	conns := c.conns
	c.conns = nil
	c.factory = nil
	c.mu.Unlock()

	if conns == nil {
		return
	}

	close(conns)
	for conn := range conns {
		conn.Close()
	}
}

func (c *Pool) Len() int {
	conns, _ := c.getConnsAndFactory()
	return len(conns)
}

// newConn wraps a standard net.Conn to a poolConn net.Conn.
func (c *Pool) wrapConn(conn net.Conn) net.Conn {
	p := &PoolConn{c: c}
	p.Conn = conn
	return p
}
