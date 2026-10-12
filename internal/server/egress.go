package server

import "net"

// egressChangeNote reports that the control-plane source IP changed while the
// stored IPv4 relay was left alone. NAT makes a locally probed address differ
// from relay_host on every cloud node, so this compares the previous connect
// IP (nodes.address before MarkNodeOnline) with the new connect IP. A domain
// relay, a declared --relay-host, a first hello, or an unchanged connect IP
// produces no note. The relay host is never rewritten.
func egressChangeNote(prevAddress, connectIP, relay, declared string, relayDeclared bool) string {
	if declared != "" || relayDeclared {
		return ""
	}
	if !ipv4Literal(relay) || !ipv4Literal(connectIP) || !ipv4Literal(prevAddress) {
		return ""
	}
	if connectIP == relay || prevAddress == connectIP {
		return ""
	}
	return "出口已变，对外地址未改（这次 " + connectIP + "，仍是 " + relay + "）"
}

func ipv4Literal(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}
