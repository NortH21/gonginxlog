package filter

import (
	"fmt"
	"net"
	"strings"

	"github.com/north21/gonginxlog/internal/record"
)

// IP matches $remote_addr against a comma-separated spec mixing exact
// addresses and CIDR subnets.
type IP struct {
	exact map[string]bool
	nets  []*net.IPNet
}

// ParseIP parses a --ip flag value.
func ParseIP(spec string) (*IP, error) {
	f := &IP{exact: map[string]bool{}}
	for _, tok := range strings.Split(spec, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.Contains(tok, "/") {
			_, ipnet, err := net.ParseCIDR(tok)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", tok, err)
			}
			f.nets = append(f.nets, ipnet)
			continue
		}
		if net.ParseIP(tok) == nil {
			return nil, fmt.Errorf("invalid IP address %q", tok)
		}
		f.exact[tok] = true
	}
	return f, nil
}

func (f *IP) Match(r *record.Record) bool {
	addr := r.RemoteAddr()
	if addr == "" {
		return false
	}
	if f.exact[addr] {
		return true
	}
	if len(f.nets) == 0 {
		return false
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	for _, n := range f.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
