// Package pool implements a pool of net.Conn to manage and reuse.
package pool

import (
	"errors"
)

var (
	// ErrClosed is the error resulting if the pool is closed via pool.Close().
	ErrClosed = errors.New("pool is closed")
)
