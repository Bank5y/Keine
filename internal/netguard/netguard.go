// Package netguard is the HTTP client Keine reaches an address a reader
// typed with (their own model, ADR 0010): it checks every IP each
// connection actually dials, so a name that resolves, or later rebinds, to
// an inside address is stopped where it connects, and it never follows a
// redirect.
//
// Public addresses are let through over https only. The deployer's
// networks let private ones through, over http too. Link-local addresses,
// where the cloud metadata services listen, are stopped whatever the
// deployer allows.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked is a connection the guard stopped. Its message names the
// address, for the server log only.
var ErrBlocked = errors.New("address not allowed")

// never are the cloud metadata services: all of link-local, and the
// addresses some clouds put them on outside it.
var never = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
	netip.MustParsePrefix("100.100.100.200/32"),
}

// inside are the addresses that are not the public internet.
var inside = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/3"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("ff00::/8"),
}

func within(ip netip.Addr, set []netip.Prefix) bool {
	for _, p := range set {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Policy is what may be dialed: the public internet over https, and the
// deployer's networks over http or https.
type Policy struct {
	Networks []netip.Prefix
}

// ParseNetworks reads the deployer's networks, CIDRs split by commas.
func ParseNetworks(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("network %q: %w", s, err)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// Allows tells whether a connection to ip may be made for a request of
// the scheme.
func (p Policy) Allows(ip netip.Addr, scheme string) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || within(ip, never) {
		return false
	}
	if within(ip, p.Networks) {
		return true
	}
	return scheme == "https" && !within(ip, inside)
}

// Client is an HTTP client that dials only what the policy allows and
// follows no redirect: the redirect comes back as the answer.
func (p Policy) Client() *http.Client {
	return &http.Client{
		Transport: &guarded{plain: p.transport("http"), tls: p.transport("https")},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (p Policy) transport(scheme string) *http.Transport {
	d := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !p.Allows(ap.Addr(), scheme) {
				return fmt.Errorf("%w: %s", ErrBlocked, address)
			}
			return nil
		},
	}
	return &http.Transport{
		// a proxy would dial for us, past the check
		Proxy:                 nil,
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// guarded sends each request over the transport of its scheme, so the
// dialer knows whether plain http is asked for.
type guarded struct{ plain, tls *http.Transport }

func (g *guarded) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Scheme {
	case "https":
		return g.tls.RoundTrip(r)
	case "http":
		return g.plain.RoundTrip(r)
	}
	return nil, fmt.Errorf("%w: scheme %q", ErrBlocked, r.URL.Scheme)
}

// Blocked tells whether err is, or wraps, a connection the guard stopped.
func Blocked(err error) bool { return errors.Is(err, ErrBlocked) }
