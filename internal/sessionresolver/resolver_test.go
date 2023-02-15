package sessionresolver

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ooni/probe-cli/v3/internal/bytecounter"
	"github.com/ooni/probe-cli/v3/internal/kvstore"
	"github.com/ooni/probe-cli/v3/internal/model"
	"github.com/ooni/probe-cli/v3/internal/model/mocks"
	"github.com/ooni/probe-cli/v3/internal/multierror"
	"github.com/ooni/probe-cli/v3/internal/netxlite"
)

func TestNetworkWorks(t *testing.T) {
	reso := &Resolver{}
	if reso.Network() != "sessionresolver" {
		t.Fatal("unexpected value returned by Network")
	}
}

func TestAddressWorks(t *testing.T) {
	reso := &Resolver{}
	if reso.Address() != "" {
		t.Fatal("unexpected value returned by Address")
	}
}

func TestTypicalUsageWithFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fail immediately
	reso := &Resolver{KVStore: &kvstore.Memory{}}
	addrs, err := reso.LookupHost(ctx, "ooni.org")
	if !errors.Is(err, ErrLookupHost) {
		t.Fatal("not the error we expected", err)
	}
	var me *multierror.Union
	if !errors.As(err, &me) {
		t.Fatal("cannot convert error")
	}
	for _, child := range me.Children {
		// net.DNSError does not include the underlying error
		// but just a string representing the error. This
		// means that we need to go down hunting what's the
		// real error that occurred and use more verbose code.
		{
			var ew *errWrapper
			if !errors.As(child, &ew) {
				t.Fatal("not an instance of errwrapper")
			}
			var de *net.DNSError
			if errors.As(ew, &de) {
				if !strings.HasSuffix(de.Err, "operation was canceled") {
					t.Fatal("not the error we expected", de.Err)
				}
				continue
			}
		}
		// otherwise just unwrap and check whether it's
		// a real context.Canceled error.
		if !errors.Is(child, context.Canceled) {
			t.Fatal("unexpected sub-error", child)
		}
	}
	if addrs != nil {
		t.Fatal("expected nil here")
	}
	if len(reso.res) < 1 {
		t.Fatal("expected to see some resolvers here")
	}
	reso.CloseIdleConnections()
	if len(reso.res) != 0 {
		t.Fatal("expected to see no resolvers after CloseIdleConnections")
	}
}

func TestTypicalUsageWithSuccess(t *testing.T) {
	expected := []string{"8.8.8.8", "8.8.4.4"}
	ctx := context.Background()
	reso := &Resolver{
		KVStore: &kvstore.Memory{},
		newChildResolverFn: func(h3 bool, URL string) (model.Resolver, error) {
			reso := &mocks.Resolver{
				MockLookupHost: func(ctx context.Context, domain string) ([]string, error) {
					return expected, nil
				},
			}
			return reso, nil
		},
	}
	addrs, err := reso.LookupHost(ctx, "dns.google")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(expected, addrs); diff != "" {
		t.Fatal(diff)
	}
}

func TestLittleLLookupHostWithInvalidURL(t *testing.T) {
	reso := &Resolver{}
	ctx := context.Background()
	ri := &resolverinfo{URL: "\t\t\t", Score: 0.99}
	addrs, err := reso.lookupHost(ctx, ri, "ooni.org")
	if err == nil || !strings.HasSuffix(err.Error(), "invalid control character in URL") {
		t.Fatal("not the error we expected", err)
	}
	if addrs != nil {
		t.Fatal("expected nil addrs here")
	}
	if ri.Score != 0 {
		t.Fatal("unexpected ri.Score", ri.Score)
	}
}

func TestLittleLLookupHostWithSuccess(t *testing.T) {
	expected := []string{"8.8.8.8", "8.8.4.4"}
	reso := &Resolver{
		newChildResolverFn: func(h3 bool, URL string) (model.Resolver, error) {
			reso := &mocks.Resolver{
				MockLookupHost: func(ctx context.Context, domain string) ([]string, error) {
					return expected, nil
				},
			}
			return reso, nil
		},
	}
	ctx := context.Background()
	ri := &resolverinfo{URL: "dot://www.ooni.nonexistent", Score: 0.1}
	addrs, err := reso.lookupHost(ctx, ri, "dns.google")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(expected, addrs); diff != "" {
		t.Fatal(diff)
	}
	if ri.Score < 0.88 || ri.Score > 0.92 {
		t.Fatal("unexpected score", ri.Score)
	}
}

func TestLittleLLookupHostWithFailure(t *testing.T) {
	errMocked := errors.New("mocked error")
	reso := &Resolver{
		newChildResolverFn: func(h3 bool, URL string) (model.Resolver, error) {
			reso := &mocks.Resolver{
				MockLookupHost: func(ctx context.Context, domain string) ([]string, error) {
					return nil, errMocked
				},
			}
			return reso, nil
		},
	}
	ctx := context.Background()
	ri := &resolverinfo{URL: "dot://www.ooni.nonexistent", Score: 0.95}
	addrs, err := reso.lookupHost(ctx, ri, "dns.google")
	if !errors.Is(err, errMocked) {
		t.Fatal("not the error we expected", err)
	}
	if addrs != nil {
		t.Fatal("expected nil addrs here")
	}
	if ri.Score < 0.094 || ri.Score > 0.096 {
		t.Fatal("unexpected score", ri.Score)
	}
}

func TestMaybeConfusionNoConfusion(t *testing.T) {
	reso := &Resolver{}
	rv := reso.maybeConfusion(nil, 0)
	if rv != -1 {
		t.Fatal("unexpected return value", rv)
	}
}

func TestMaybeConfusionNoArray(t *testing.T) {
	reso := &Resolver{}
	rv := reso.maybeConfusion(nil, 11)
	if rv != 0 {
		t.Fatal("unexpected return value", rv)
	}
}

func TestMaybeConfusionSingleEntry(t *testing.T) {
	reso := &Resolver{}
	state := []*resolverinfo{{}}
	rv := reso.maybeConfusion(state, 11)
	if rv != 0 {
		t.Fatal("unexpected return value", rv)
	}
}

func TestMaybeConfusionTwoEntries(t *testing.T) {
	reso := &Resolver{}
	state := []*resolverinfo{{
		Score: 0.8,
		URL:   "https://dns.google/dns-query",
	}, {
		Score: 0.4,
		URL:   "http3://dns.google/dns-query",
	}}
	rv := reso.maybeConfusion(state, 11)
	if rv != 2 {
		t.Fatal("unexpected return value", rv)
	}
	if state[0].Score != 0.4 {
		t.Fatal("unexpected state[0].Score")
	}
	if state[0].URL != "http3://dns.google/dns-query" {
		t.Fatal("unexpected state[0].URL")
	}
	if state[1].Score != 0.8 {
		t.Fatal("unexpected state[1].Score")
	}
	if state[1].URL != "https://dns.google/dns-query" {
		t.Fatal("unexpected state[1].URL")
	}
}

func TestMaybeConfusionManyEntries(t *testing.T) {
	reso := &Resolver{}
	state := []*resolverinfo{{
		Score: 0.8,
		URL:   "https://dns.google/dns-query",
	}, {
		Score: 0.4,
		URL:   "http3://dns.google/dns-query",
	}, {
		Score: 0.1,
		URL:   "system:///",
	}, {
		Score: 0.01,
		URL:   "dot://dns.google",
	}}
	rv := reso.maybeConfusion(state, 11)
	if rv != 3 {
		t.Fatal("unexpected return value", rv)
	}
	if state[0].Score != 0.1 {
		t.Fatal("unexpected state[0].Score")
	}
	if state[0].URL != "system:///" {
		t.Fatal("unexpected state[0].URL")
	}
	if state[1].Score != 0.4 {
		t.Fatal("unexpected state[1].Score")
	}
	if state[1].URL != "http3://dns.google/dns-query" {
		t.Fatal("unexpected state[1].URL")
	}
	if state[2].Score != 0.8 {
		t.Fatal("unexpected state[2].Score")
	}
	if state[2].URL != "https://dns.google/dns-query" {
		t.Fatal("unexpected state[2].URL")
	}
	if state[3].Score != 0.01 {
		t.Fatal("unexpected state[3].Score")
	}
	if state[3].URL != "dot://dns.google" {
		t.Fatal("unexpected state[3].URL")
	}
}

func TestResolverWorksWithProxy(t *testing.T) {
	var (
		works      = &atomic.Int64{}
		startuperr = make(chan error)
		listench   = make(chan net.Listener)
		done       = make(chan interface{})
	)
	// proxy implementation
	go func() {
		defer close(done)
		lconn, err := net.Listen("tcp", "127.0.0.1:0")
		startuperr <- err
		if err != nil {
			return
		}
		listench <- lconn
		for {
			conn, err := lconn.Accept()
			if err != nil {
				// We assume this is when we were told to
				// shutdown by the main goroutine.
				return
			}
			works.Add(1)
			conn.Close()
		}
	}()
	// make sure we could start the proxy
	if err := <-startuperr; err != nil {
		t.Fatal(err)
	}
	listener := <-listench
	// use the proxy
	reso := &Resolver{
		ProxyURL: &url.URL{
			Scheme: "socks5",
			Host:   listener.Addr().String(),
		},
		KVStore: &kvstore.Memory{},
	}
	ctx := context.Background()
	addrs, err := reso.LookupHost(ctx, "ooni.org")
	// cleanly shutdown the listener
	listener.Close()
	<-done
	// check results
	if !errors.Is(err, ErrLookupHost) {
		t.Fatal("not the error we expected", err)
	}
	if addrs != nil {
		t.Fatal("expected nil addrs")
	}
	if works.Load() < 1 {
		t.Fatal("expected to see a positive number of entries here")
	}
}

func TestShouldSkipWithProxyWorks(t *testing.T) {
	expect := []struct {
		url    string
		result bool
	}{{
		url:    "\t",
		result: true,
	}, {
		url:    "https://dns.google/dns-query",
		result: false,
	}, {
		url:    "dot://dns.google/",
		result: false,
	}, {
		url:    "http3://dns.google/dns-query",
		result: true,
	}, {
		url:    "tcp://dns.google/",
		result: false,
	}, {
		url:    "udp://dns.google/",
		result: true,
	}, {
		url:    "system:///",
		result: true,
	}}
	reso := &Resolver{}
	for _, e := range expect {
		out := reso.shouldSkipWithProxy(&resolverinfo{URL: e.url})
		if out != e.result {
			t.Fatal("unexpected result for", e)
		}
	}
}

func TestUnimplementedFunctions(t *testing.T) {
	t.Run("LookupHTTPS", func(t *testing.T) {
		r := &Resolver{}
		https, err := r.LookupHTTPS(context.Background(), "dns.google")
		if !errors.Is(err, errLookupNotImplemented) {
			t.Fatal("unexpected error", err)
		}
		if https != nil {
			t.Fatal("expected nil result")
		}
	})

	t.Run("LookupNS", func(t *testing.T) {
		r := &Resolver{}
		ns, err := r.LookupNS(context.Background(), "dns.google")
		if !errors.Is(err, errLookupNotImplemented) {
			t.Fatal("unexpected error", err)
		}
		if len(ns) > 0 {
			t.Fatal("expected empty result")
		}
	})
}

func TestResolverWorkingAsIntendedWithMocks(t *testing.T) {

	// fields contains the public fields to set.
	type fields struct {
		// byteCounter is the byte counter we'll use.
		byteCounter *bytecounter.Counter

		// kvstore is the kvstore we'll use.
		kvstore *kvstore.Memory

		// logger is the logger we'll use.
		logger model.Logger

		// proxyURL is the proxy URL we'll use.
		proxyURL *url.URL
	}

	// testCase is an individual test case.
	type testCase struct {
		// name is the test case name.
		name string

		// fields contains the fields to set.
		fields *fields

		// domainToResolve is the domain to resolve.
		domainToResolve string

		// tproxy contains the netxlite underlying network
		// configuration to use for testing.
		tproxy model.UnderlyingNetwork

		// expectErr indicates whether we expected an error.
		expectErr bool

		// expectAddrs contains the expected addresses.
		expectAddrs []string
	}

	// TODO(bassosimone): as painful as it may be, we need to write more
	// tests like this that capture the whole behavior of the package. They
	// give us higher confidence that _everything_ is still WAI regardless
	// of any intermediate refactoring we may be implement here.
	//
	// For now, I have just written tests for extreme use cases such as
	// nothing is working and just the system resolver is working. We need
	// to figure out a way of writing more alike tests.
	//
	// I am not going to do that now, because that would be out of the
	// scope of the current pull request on which I am working.

	var testCases = []testCase{{
		name: "every system-resolver lookup returns NXDOMAIN",
		fields: &fields{
			byteCounter: bytecounter.New(),
			kvstore:     &kvstore.Memory{},
			logger:      model.DiscardLogger,
			proxyURL:    nil,
		},
		domainToResolve: "example.com",
		tproxy: &mocks.UnderlyingNetwork{
			MockDefaultCertPool: func() *x509.CertPool {
				return netxlite.NewDefaultCertPool()
			},
			MockDialContext: func(ctx context.Context, timeout time.Duration, network string, address string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: timeout}
				return dialer.DialContext(ctx, network, address)
			},
			MockListenUDP: func(network string, addr *net.UDPAddr) (model.UDPLikeConn, error) {
				return net.ListenUDP(network, addr)
			},
			MockGetaddrinfoLookupANY: func(ctx context.Context, domain string) ([]string, string, error) {
				return nil, "", errors.New(netxlite.DNSNoSuchHostSuffix)
			},
			MockGetaddrinfoResolverNetwork: func() string {
				return netxlite.StdlibResolverGetaddrinfo
			},
		},
		expectErr:   true,
		expectAddrs: nil,
	}, {
		name: "only the system resolver works",
		fields: &fields{
			byteCounter: bytecounter.New(),
			kvstore:     &kvstore.Memory{},
			logger:      model.DiscardLogger,
			proxyURL:    nil,
		},
		domainToResolve: "example.com",
		tproxy: &mocks.UnderlyingNetwork{
			MockDefaultCertPool: func() *x509.CertPool {
				return netxlite.NewDefaultCertPool()
			},
			MockDialContext: func(ctx context.Context, timeout time.Duration, network string, address string) (net.Conn, error) {
				return nil, syscall.ECONNREFUSED
			},
			MockListenUDP: func(network string, addr *net.UDPAddr) (model.UDPLikeConn, error) {
				return nil, syscall.ENETDOWN
			},
			MockGetaddrinfoLookupANY: func(ctx context.Context, domain string) ([]string, string, error) {
				switch domain {
				case "example.com":
					return []string{"1.2.3.4", "1.2.3.5"}, "", nil
				default:
					return nil, "", errors.New(netxlite.DNSNoSuchHostSuffix)
				}
			},
			MockGetaddrinfoResolverNetwork: func() string {
				return netxlite.StdlibResolverGetaddrinfo
			},
		},
		expectErr:   false,
		expectAddrs: []string{"1.2.3.4", "1.2.3.5"},
	}}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			netxlite.WithCustomTProxy(tt.tproxy, func() {
				reso := &Resolver{
					ByteCounter: tt.fields.byteCounter,
					KVStore:     tt.fields.kvstore,
					Logger:      tt.fields.logger,
					ProxyURL:    tt.fields.proxyURL,
				}

				addrs, err := reso.LookupHost(context.Background(), tt.domainToResolve)
				if (err != nil) != tt.expectErr {
					t.Fatal("tt.expectErr", tt.expectErr, "got", err)
				}

				if diff := cmp.Diff(tt.expectAddrs, addrs); diff != "" {
					t.Fatal(diff)
				}
			})
		})
	}
}
