package dns

import (
	"net"
	"strings"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
)

// TestUDPOnlyWhenTCPPortTaken mirrors the work-PC reality: wslrelay (or
// similar) owns TCP 53 while UDP is bindable. The server must come up in
// UDP-only mode and still answer.
func TestUDPOnlyWhenTCPPortTaken(t *testing.T) {
	// Find a port where we can occupy TCP first.
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.LocalAddr().String()
	_ = probe.Close()

	blocker, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("could not occupy tcp %s: %v", addr, err)
	}
	defer func() { _ = blocker.Close() }()

	s := &Server{TLD: "test", Addr: addr}
	if err := s.Start(); err != nil {
		t.Fatalf("Start should succeed UDP-only: %v", err)
	}
	t.Cleanup(s.Stop)
	if s.TCPErr == nil || !strings.Contains(s.TCPErr.Error(), "tcp") {
		t.Errorf("TCPErr = %v, want tcp bind error", s.TCPErr)
	}

	c := &mdns.Client{Timeout: 3 * time.Second}
	m := new(mdns.Msg)
	m.SetQuestion("udponly.test.", mdns.TypeA)
	resp, _, err := c.Exchange(m, s.LocalAddr())
	if err != nil {
		t.Fatalf("udp query failed: %v", err)
	}
	if len(resp.Answer) != 1 {
		t.Errorf("answers = %d", len(resp.Answer))
	}
}
