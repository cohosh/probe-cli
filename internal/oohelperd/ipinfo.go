package oohelperd

//
// Generates IP and endpoint information.
//

import (
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/ooni/probe-cli/v3/internal/geoipx"
	"github.com/ooni/probe-cli/v3/internal/model"
	"github.com/ooni/probe-cli/v3/internal/netxlite"
)

// newIPInfo creates an IP to IPInfo mapping from addresses resolved
// by the probe (inside [creq]) or the TH (inside [addrs]).
func newIPInfo(creq *ctrlRequest, addrs []string) map[string]*model.THIPInfo {
	discoveredby := make(map[string]int64)

	for _, epnt := range creq.TCPConnect {
		addr, _, err := net.SplitHostPort(epnt)
		if err != nil || net.ParseIP(addr) == nil {
			continue
		}
		discoveredby[addr] |= model.THIPInfoFlagResolvedByProbe
	}

	for _, addr := range addrs {
		if net.ParseIP(addr) != nil {
			discoveredby[addr] |= model.THIPInfoFlagResolvedByTH
		}
	}

	ipinfo := make(map[string]*model.THIPInfo)
	for addr, flags := range discoveredby {
		if netxlite.IsBogon(addr) { // note: we already excluded non-IP addrs above
			flags |= model.THIPInfoFlagIsBogon
		}
		asn, _, _ := geoipx.LookupASN(addr) // AS0 on failure
		ipinfo[addr] = &model.THIPInfo{
			ASN:   int64(asn),
			Flags: flags,
		}
	}

	return ipinfo
}

// endpointInfo contains info about an endpoint to measure
type endpointInfo struct {
	// Addr is the address to measure
	Addr string

	// Epnt is the endpoint to measure
	Epnt string

	// TLS indicates whether we should try using TLS
	TLS bool
}

// ipInfoToEndpoints takes in input the [ipinfo] returned by newIPInfo
// and the [URL] provided by the probe to generate the list of endpoints
// to measure. We choose ports as follows:
//
// 1. if the input URL contains a port, we use such a port;
//
// 2. if the input URL scheme is "https", we choose port 443;
//
// 3. if the input URL scheme is "http", we use both 443 and 80, which
// allows us to include in the measurement information useful to determine
// whether an IP address is valid for a domain;
//
// 4. otherwise, we don't generate any endpoint to measure.
func ipInfoToEndpoints(URL *url.URL, ipinfo map[string]*model.THIPInfo) []endpointInfo {
	var ports []string

	if port := URL.Port(); port != "" {
		ports = []string{port} // as documented
	} else if URL.Scheme == "https" {
		ports = []string{"443"} // as documented
	} else if URL.Scheme == "http" {
		ports = []string{"80", "443"} // as documented
	}

	out := []endpointInfo{}
	for addr, info := range ipinfo {
		if (info.Flags & model.THIPInfoFlagIsBogon) != 0 {
			continue // as documented
		}
		for _, port := range ports {
			epnt := net.JoinHostPort(addr, port)
			out = append(out, endpointInfo{
				Addr: addr,
				Epnt: epnt,
				TLS:  port == "443",
			})
		}
	}

	// sort the output to make testing work deterministically since iterating
	// a map in golang isn't guaranteed to return ordered keys
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Compare(out[i].Epnt, out[j].Epnt) < 0
	})

	return out
}
