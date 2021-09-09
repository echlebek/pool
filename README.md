# Fork of an archived project. Minimal maintenance. 
This project is a fork of github.com/fatih/pool. It was forked so that it could become a Go module. Since Go module versions are largely incompatible with previously understood vendor tool ideas about semantic versions, the previous v1.0.0, v2.0.0, and v3.0.0 tags have also been deleted. The module's versions now begin at v0.1.0.

The module's API and behaviour has changed from the original. It now supports context.Context in the pool.Get method, and blocks when the pool cannot provide any more connections without growing. The pool can also be dynamically resized with SetCap(). Additionally, the Pool interface has been removed and the channelPool concrete type has been exported as Pool.

Thanks all for their work on this project. 

Pool is a goroutine-safe connection pool for net.Conn. It can be used to
manage and reuse connections.

## Install and Usage

Install the package with:

```bash
go get github.com/echlebek/pool
```

## Example

```go
// create a factory() to be used with channel based pool
factory    := func() (net.Conn, error) { return net.Dial("tcp", "127.0.0.1:4000") }

// create a new channel based pool with an initial capacity of 5 and maximum
// capacity of 30. The factory will create 5 initial connections and put it
// into the pool.
p, err := pool.NewChannelPool(5, 30, factory)

// now you can get a connection from the pool, if there is no connection
// available it will create a new one via the factory function.
conn, err := p.Get(context.Background())

// do something with conn and put it back to the pool by closing the connection
// (this doesn't close the underlying connection instead it's putting it back
// to the pool).
conn.Close()

// close the underlying connection instead of returning it to pool
// it is useful when acceptor has already closed connection and conn.Write() returns error
if pc, ok := conn.(*pool.PoolConn); ok {
  pc.MarkUnusable()
  pc.Close()
}

// close pool any time you want, this closes all the connections inside a pool
p.Close()

// currently available connections in the pool
current := p.Len()
```


## Credits

 * [Fatih Arslan](https://github.com/fatih)
 * [sougou](https://github.com/sougou)

## License

The MIT License (MIT) - see LICENSE for more details
