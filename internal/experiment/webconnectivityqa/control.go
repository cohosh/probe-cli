package webconnectivityqa

import (
	"github.com/apex/log"
	"github.com/ooni/netem"
	"github.com/ooni/probe-cli/v3/internal/netemx"
)

// controlFailureWithSuccessfulHTTPWebsite verifies that we correctly handle the case
// where we cannot reach the control server but the website measurement is OK.
func controlFailureWithSuccessfulHTTPWebsite() *TestCase {
	return &TestCase{
		Name:  "controlFailureWithSuccessfulHTTPWebsite",
		Flags: TestCaseFlagNoLTE, // BUG: has "consistent" DNS but still blocking=null and accessible=null
		Input: "http://www.example.org/",
		Configure: func(env *netemx.QAEnv) {

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "0.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "1.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "2.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "3.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "d33d1gs9kpq1c5.cloudfront.net",
			})

		},
		ExpectErr: false,
		ExpectTestKeys: &testKeys{
			ControlFailure: "unknown_failure: httpapi: all endpoints failed: [ connection_reset; connection_reset; connection_reset; connection_reset;]",
			XStatus:        8, // StatusAnomalyControlUnreachable
			Accessible:     nil,
			Blocking:       nil,
		},
	}
}

// controlFailureWithSuccessfulHTTPSWebsite verifies that we correctly handle the case
// where we cannot reach the control server but the website measurement is OK.
func controlFailureWithSuccessfulHTTPSWebsite() *TestCase {
	return &TestCase{
		Name:  "controlFailureWithSuccessfulHTTPSWebsite",
		Flags: TestCaseFlagNoLTE, // because it (correctly!) sets the DNS as consistent thanks to TLS
		Input: "https://www.example.org/",
		Configure: func(env *netemx.QAEnv) {

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "0.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "1.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "2.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "3.th.ooni.org",
			})

			env.DPIEngine().AddRule(&netem.DPIResetTrafficForTLSSNI{
				Logger: log.Log,
				SNI:    "d33d1gs9kpq1c5.cloudfront.net",
			})

		},
		ExpectErr: false,
		ExpectTestKeys: &testKeys{
			ControlFailure: "unknown_failure: httpapi: all endpoints failed: [ connection_reset; connection_reset; connection_reset; connection_reset;]",
			XStatus:        1, // StatusSuccessSecure
			XNullNullFlags: 8, // analysisFlagNullNullSuccessfulHTTPS
			Accessible:     true,
			Blocking:       false,
		},
	}
}
