package measurexlite

//
// Conn tracing
//

import (
	"fmt"
	"net"
	"time"

	"github.com/ooni/probe-cli/v3/internal/model"
	"github.com/ooni/probe-cli/v3/internal/netxlite"
)

// MaybeClose is a convenience function for closing a [net.Conn] when it is not nil.
func MaybeClose(conn net.Conn) (err error) {
	if conn != nil {
		err = conn.Close()
	}
	return
}

// MaybeWrapNetConn implements model.Trace.MaybeWrapNetConn.
func (tx *Trace) MaybeWrapNetConn(conn net.Conn) net.Conn {
	return &connTrace{
		Conn: conn,
		tx:   tx,
	}
}

// connTrace is a trace-aware net.Conn.
type connTrace struct {
	// Implementation note: it seems safe to use embedding here because net.Conn
	// is an interface from the standard library that we don't control
	net.Conn
	tx *Trace
}

var _ net.Conn = &connTrace{}

// Read implements net.Conn.Read and saves network events.
func (c *connTrace) Read(b []byte) (int, error) {
	// collect preliminary stats when the connection is surely active
	network := c.RemoteAddr().Network()
	addr := c.RemoteAddr().String()
	started := c.tx.TimeSince(c.tx.ZeroTime)

	// perform the underlying network operation
	count, err := c.Conn.Read(b)

	// emit the network event
	finished := c.tx.TimeSince(c.tx.ZeroTime)
	select {
	case c.tx.networkEvent <- NewArchivalNetworkEvent(
		c.tx.Index, started, netxlite.ReadOperation, network, addr, count,
		err, finished, c.tx.tags...):
	default: // buffer is full
	}

	// update per receiver statistics
	c.tx.updateBytesReceivedMapNetConn(network, addr, count)

	// return to the caller
	return count, err
}

// updateBytesReceivedMapNetConn updates the [*Trace] bytes received map for a [net.Conn].
func (tx *Trace) updateBytesReceivedMapNetConn(network, address string, count int) {
	// normalize the network name
	switch network {
	case "udp", "udp4", "udp6":
		network = "udp"
	case "tcp", "tcp4", "tcp6":
		network = "tcp"
	}

	// create the key for inserting inside the map
	key := fmt.Sprintf("%s %s", address, network)

	// lock and insert into the map
	tx.bytesReceivedMu.Lock()
	tx.bytesReceivedMap[key] += int64(count)
	tx.bytesReceivedMu.Unlock()
}

// CloneBytesReceivedMap returns a clone of the internal bytes received map. The key
// of the map is a string following the "EPNT_ADDRESS PROTO" pattern where the "EPNT_ADDRESS"
// contains the endpoint address and "PROTO" is "tcp" or "udp".
func (tx *Trace) CloneBytesReceivedMap() (out map[string]int64) {
	out = make(map[string]int64)
	tx.bytesReceivedMu.Lock()
	for key, value := range tx.bytesReceivedMap {
		out[key] = value
	}
	tx.bytesReceivedMu.Unlock()
	return
}

// Write implements net.Conn.Write and saves network events.
func (c *connTrace) Write(b []byte) (int, error) {
	network := c.RemoteAddr().Network()
	addr := c.RemoteAddr().String()
	started := c.tx.TimeSince(c.tx.ZeroTime)

	count, err := c.Conn.Write(b)

	finished := c.tx.TimeSince(c.tx.ZeroTime)
	select {
	case c.tx.networkEvent <- NewArchivalNetworkEvent(
		c.tx.Index, started, netxlite.WriteOperation, network, addr, count,
		err, finished, c.tx.tags...):
	default: // buffer is full
	}

	return count, err
}

// MaybeCloseUDPLikeConn is a convenience function for closing a [model.UDPLikeConn] when it is not nil.
func MaybeCloseUDPLikeConn(conn model.UDPLikeConn) (err error) {
	if conn != nil {
		err = conn.Close()
	}
	return
}

// MaybeWrapUDPLikeConn implements model.Trace.MaybeWrapUDPLikeConn.
func (tx *Trace) MaybeWrapUDPLikeConn(conn model.UDPLikeConn) model.UDPLikeConn {
	return &udpLikeConnTrace{
		UDPLikeConn: conn,
		tx:          tx,
	}
}

// udpLikeConnTrace is a trace-aware model.UDPLikeConn.
type udpLikeConnTrace struct {
	// Implementation note: it seems ~safe to use embedding here because model.UDPLikeConn
	// contains fields deriving from how quic-go/quic-go uses the standard library
	model.UDPLikeConn
	tx *Trace
}

// Read implements model.UDPLikeConn.ReadFrom and saves network events.
func (c *udpLikeConnTrace) ReadFrom(b []byte) (int, net.Addr, error) {
	// record when we started measuring
	started := c.tx.TimeSince(c.tx.ZeroTime)

	// perform the network operation
	count, addr, err := c.UDPLikeConn.ReadFrom(b)

	// emit the network event
	finished := c.tx.TimeSince(c.tx.ZeroTime)
	address := addrStringIfNotNil(addr)
	select {
	case c.tx.networkEvent <- NewArchivalNetworkEvent(
		c.tx.Index, started, netxlite.ReadFromOperation, "udp", address, count,
		err, finished, c.tx.tags...):
	default: // buffer is full
	}

	// possibly collect a download speed sample
	c.tx.maybeUpdateBytesReceivedMapUDPLikeConn(addr, count)

	// return results to the caller
	return count, addr, err
}

// maybeUpdateBytesReceivedMapUDPLikeConn updates the [*Trace] bytes received map for a [model.UDPLikeConn].
func (tx *Trace) maybeUpdateBytesReceivedMapUDPLikeConn(addr net.Addr, count int) {
	// Implementation note: the address may be nil if the operation failed given that we don't
	// have a fixed peer address for UDP connections
	if addr != nil {
		tx.updateBytesReceivedMapNetConn(addr.Network(), addr.String(), count)
	}
}

// Write implements model.UDPLikeConn.WriteTo and saves network events.
func (c *udpLikeConnTrace) WriteTo(b []byte, addr net.Addr) (int, error) {
	started := c.tx.TimeSince(c.tx.ZeroTime)
	address := addr.String()

	count, err := c.UDPLikeConn.WriteTo(b, addr)

	finished := c.tx.TimeSince(c.tx.ZeroTime)
	select {
	case c.tx.networkEvent <- NewArchivalNetworkEvent(
		c.tx.Index, started, netxlite.WriteToOperation, "udp", address, count,
		err, finished, c.tx.tags...):
	default: // buffer is full
	}

	return count, err
}

// addrStringIfNotNil returns the string of the given addr
// unless the addr is nil, in which case it returns an empty string.
func addrStringIfNotNil(addr net.Addr) (out string) {
	if addr != nil {
		out = addr.String()
	}
	return
}

// NewArchivalNetworkEvent creates a new model.ArchivalNetworkEvent.
func NewArchivalNetworkEvent(index int64, started time.Duration, operation string,
	network string, address string, count int, err error, finished time.Duration,
	tags ...string) *model.ArchivalNetworkEvent {
	return &model.ArchivalNetworkEvent{
		Address:       address,
		Failure:       NewFailure(err),
		NumBytes:      int64(count),
		Operation:     operation,
		Proto:         network,
		T0:            started.Seconds(),
		T:             finished.Seconds(),
		TransactionID: index,
		Tags:          copyAndNormalizeTags(tags),
	}
}

// NewAnnotationArchivalNetworkEvent is a simplified NewArchivalNetworkEvent
// where we create a simple annotation without attached I/O info.
func NewAnnotationArchivalNetworkEvent(
	index int64, time time.Duration, operation string, tags ...string) *model.ArchivalNetworkEvent {
	return NewArchivalNetworkEvent(index, time, operation, "", "", 0, nil, time, tags...)
}

// NetworkEvents drains the network events buffered inside the NetworkEvent channel.
func (tx *Trace) NetworkEvents() (out []*model.ArchivalNetworkEvent) {
	for {
		select {
		case ev := <-tx.networkEvent:
			out = append(out, ev)
		default:
			return // done
		}
	}
}

// FirstNetworkEventOrNil drains the network events buffered inside the NetworkEvents channel
// and returns the first NetworkEvent, if any. Otherwise, it returns nil.
func (tx *Trace) FirstNetworkEventOrNil() *model.ArchivalNetworkEvent {
	ev := tx.NetworkEvents()
	if len(ev) < 1 {
		return nil
	}
	return ev[0]
}

// copyAndNormalizeTags ensures that we map nil tags to []string
// and that we return a copy of the tags.
func copyAndNormalizeTags(tags []string) []string {
	if len(tags) <= 0 {
		tags = []string{}
	}
	return append([]string{}, tags...)
}
