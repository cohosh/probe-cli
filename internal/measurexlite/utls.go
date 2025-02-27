package measurexlite

//
// Support for using utls
//

import (
	"github.com/ooni/probe-cli/v3/internal/model"
	utls "gitlab.com/yawning/utls.git"
)

// NewTLSHandshakerUTLS is equivalent to netxlite.NewTLSHandshakerUTLS
// except that it returns a model.TLSHandshaker that uses this trace.
func (tx *Trace) NewTLSHandshakerUTLS(dl model.DebugLogger, id *utls.ClientHelloID) model.TLSHandshaker {
	return &tlsHandshakerTrace{
		thx: tx.Netx.NewTLSHandshakerUTLS(dl, id),
		tx:  tx,
	}
}
