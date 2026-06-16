package dns

import (
	"net"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
)

func startTestServer(t *testing.T) *Server {
	t.Helper()
	// The OS picks the UDP port; the matching TCP port may be reserved
	// (Windows excluded ranges) — retry until both protocols bind.
	for attempt := 0; attempt < 10; attempt++ {
		s := &Server{TLD: "test", Addr: "127.0.0.1:0"}
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		if s.TCPErr == nil {
			t.Cleanup(s.Stop)
			return s
		}
		s.Stop()
	}
	t.Skip("could not find a port with both UDP and TCP free")
	return nil
}

func query(t *testing.T, addr, name string, qtype uint16) *mdns.Msg {
	t.Helper()
	c := &mdns.Client{Timeout: 3 * time.Second}
	m := new(mdns.Msg)
	m.SetQuestion(mdns.Fqdn(name), qtype)
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	return resp
}

func TestWildcardA(t *testing.T) {
	s := startTestServer(t)
	for _, name := range []string{"myapp.test", "deep.sub.myapp.test", "MiXeD.TeSt"} {
		resp := query(t, s.LocalAddr(), name, mdns.TypeA)
		if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 1 {
			t.Fatalf("%s: rcode=%d answers=%d", name, resp.Rcode, len(resp.Answer))
		}
		a, ok := resp.Answer[0].(*mdns.A)
		if !ok || !a.A.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Errorf("%s: answer = %v", name, resp.Answer[0])
		}
	}
}

func TestWildcardAAAA(t *testing.T) {
	s := startTestServer(t)
	resp := query(t, s.LocalAddr(), "myapp.test", mdns.TypeAAAA)
	if len(resp.Answer) != 1 {
		t.Fatalf("answers = %d", len(resp.Answer))
	}
	aaaa, ok := resp.Answer[0].(*mdns.AAAA)
	if !ok || !aaaa.AAAA.Equal(net.IPv6loopback) {
		t.Errorf("answer = %v", resp.Answer[0])
	}
}

func TestOtherZonesRefused(t *testing.T) {
	s := startTestServer(t)
	for _, name := range []string{"example.com", "test.example.org", "anothertest"} {
		resp := query(t, s.LocalAddr(), name, mdns.TypeA)
		if resp.Rcode != mdns.RcodeRefused {
			t.Errorf("%s: rcode = %d, want REFUSED", name, resp.Rcode)
		}
	}
}

func TestUnsupportedTypeEmptyNoError(t *testing.T) {
	s := startTestServer(t)
	resp := query(t, s.LocalAddr(), "myapp.test", mdns.TypeMX)
	if resp.Rcode != mdns.RcodeSuccess || len(resp.Answer) != 0 {
		t.Errorf("MX: rcode=%d answers=%d, want NOERROR/0", resp.Rcode, len(resp.Answer))
	}
}

func TestTCPAlsoServes(t *testing.T) {
	s := startTestServer(t)
	c := &mdns.Client{Net: "tcp", Timeout: 3 * time.Second}
	m := new(mdns.Msg)
	m.SetQuestion("myapp.test.", mdns.TypeA)
	resp, _, err := c.Exchange(m, s.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Answer) != 1 {
		t.Errorf("tcp answers = %d", len(resp.Answer))
	}
}
