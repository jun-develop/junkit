package security

import "net/netip"

// ClientKey returns the key to limit or match a client by: the /64 prefix of an IPv6 address,
// which one client usually holds whole, so rotating addresses inside it does not reset a limit;
// an IPv4-mapped IPv6 address becomes its IPv4 form, and IPv4 or unparsable values are returned
// unchanged.
//
// Usage:
//
//	key := "login:ip:" + security.ClientKey(c.ClientIP())
func ClientKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	switch {
	case err != nil, addr.Is4():
		return ip
	case addr.Is4In6():
		return addr.Unmap().String()
	}

	prefix, _ := addr.WithZone("").Prefix(64)

	return prefix.String()
}
