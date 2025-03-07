package netxlite

//
// Code to use yawning/utls or refraction-networking/utls
//

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"reflect"

	"github.com/ooni/probe-cli/v3/internal/model"
	utls "gitlab.com/yawning/utls.git"
)

// NewTLSHandshakerUTLS implements [model.MeasuringNetwork].
func (netx *Netx) NewTLSHandshakerUTLS(logger model.DebugLogger, id *utls.ClientHelloID) model.TLSHandshaker {
	return newTLSHandshakerLogger(&tlsHandshakerConfigurable{
		NewConn:  newUTLSConnFactory(id),
		provider: netx.MaybeCustomUnderlyingNetwork(),
	}, logger)
}

// NewTLSHandshakerUTLS is equivalent to creating an empty [*Netx]
// and calling its NewTLSHandshakerUTLS method.
func NewTLSHandshakerUTLS(logger model.DebugLogger, id *utls.ClientHelloID) model.TLSHandshaker {
	netx := &Netx{Underlying: nil}
	return netx.NewTLSHandshakerUTLS(logger, id)
}

// UTLSConn implements TLSConn and uses a utls UConn as its underlying connection
type UTLSConn struct {
	// We include the real UConn
	*utls.UConn

	// This field helps with writing tests
	testableHandshake func() error

	// Required by NetConn
	nc net.Conn
}

// Ensures that a UTLSConn implements the TLSConn interface.
var _ TLSConn = &UTLSConn{}

// newUTLSConnFactory returns a NewConn function for creating UTLSConn instances.
func newUTLSConnFactory(clientHello *utls.ClientHelloID) func(conn net.Conn, config *tls.Config) (TLSConn, error) {
	return func(conn net.Conn, config *tls.Config) (TLSConn, error) {
		return NewUTLSConn(conn, config, clientHello)
	}
}

// errUTLSIncompatibleStdlibConfig indicates that the stdlib config you passed to
// NewUTLSConn contains some fields we don't support.
var errUTLSIncompatibleStdlibConfig = errors.New("utls: incompatible stdlib config")

// NewUTLSConn creates a new connection with the given client hello ID.
func NewUTLSConn(conn net.Conn, config *tls.Config, cid *utls.ClientHelloID) (*UTLSConn, error) {
	supportedFields := map[string]bool{
		"DynamicRecordSizingDisabled": true,
		"InsecureSkipVerify":          true,
		"NextProtos":                  true,
		"RootCAs":                     true,
		"ServerName":                  true,
	}
	value := reflect.ValueOf(config).Elem()
	kind := value.Type()
	for idx := 0; idx < value.NumField(); idx++ {
		field := value.Field(idx)
		if field.IsZero() {
			continue
		}
		fieldKind := kind.Field(idx)
		if supportedFields[fieldKind.Name] {
			continue
		}
		err := fmt.Errorf("%w: field %s is nonzero", errUTLSIncompatibleStdlibConfig, fieldKind.Name)
		return nil, err
	}
	uConfig := &utls.Config{
		DynamicRecordSizingDisabled: config.DynamicRecordSizingDisabled,
		InsecureSkipVerify:          config.InsecureSkipVerify,
		RootCAs:                     config.RootCAs,
		NextProtos:                  config.NextProtos,
		ServerName:                  config.ServerName,
	}
	tlsConn := utls.UClient(conn, uConfig, *cid)
	oconn := &UTLSConn{
		UConn:             tlsConn,
		testableHandshake: nil,
		nc:                conn,
	}
	return oconn, nil
}

// ErrUTLSHandshakePanic indicates that there was panic handshaking
// when we were using the yawning/utls library for parroting.
// See https://github.com/ooni/probe/issues/1770 for more information.
var ErrUTLSHandshakePanic = errors.New("utls: handshake panic")

func (c *UTLSConn) HandshakeContext(ctx context.Context) (err error) {
	errch := make(chan error, 1)
	go func() {
		defer func() {
			// See https://github.com/ooni/probe/issues/1770
			if recover() != nil {
				errch <- ErrUTLSHandshakePanic
			}
		}()
		errch <- c.handshakefn()()
	}()
	select {
	case err = <-errch:
	case <-ctx.Done():
		err = ctx.Err()
	}
	return
}

func (c *UTLSConn) handshakefn() func() error {
	if c.testableHandshake != nil {
		return c.testableHandshake
	}
	return c.UConn.Handshake
}

func (c *UTLSConn) ConnectionState() tls.ConnectionState {
	uState := c.Conn.ConnectionState()
	return tls.ConnectionState{
		Version:                     uState.Version,
		HandshakeComplete:           uState.HandshakeComplete,
		DidResume:                   uState.DidResume,
		CipherSuite:                 uState.CipherSuite,
		NegotiatedProtocol:          uState.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  uState.NegotiatedProtocolIsMutual,
		ServerName:                  uState.ServerName,
		PeerCertificates:            uState.PeerCertificates,
		VerifiedChains:              uState.VerifiedChains,
		SignedCertificateTimestamps: uState.SignedCertificateTimestamps,
		OCSPResponse:                uState.OCSPResponse,
		TLSUnique:                   uState.TLSUnique,
	}
}

func (c *UTLSConn) NetConn() net.Conn {
	return c.nc
}
