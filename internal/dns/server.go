package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	mdns "github.com/miekg/dns"
)

// Server answers every name under *.<tld> with 127.0.0.1 / ::1 and refuses
// anything else — it is only ever registered with the OS for the Hull TLD
// (NRPT rule, /etc/resolver file, or resolved routing domain), so other
// zones must never be answered here.
type Server struct {
	// TLD without a leading dot, e.g. "test".
	TLD string
	// Addr to listen on, e.g. "127.0.0.1:53" ("127.0.0.1:0" in tests).
	Addr string

	udp *mdns.Server
	tcp *mdns.Server

	// TCPErr records a failed TCP bind, leaving the server in UDP-only
	// mode. Common on Windows where wslrelay holds TCP 127.0.0.1:53.
	// UDP alone serves resolver lookups — Hull's answers never truncate.
	TCPErr error
}

// Start binds UDP and TCP listeners and serves in the background.
func (s *Server) Start() error {
	zone := "." + strings.Trim(strings.ToLower(s.TLD), ".") + "."

	handler := mdns.HandlerFunc(func(w mdns.ResponseWriter, r *mdns.Msg) {
		m := new(mdns.Msg)
		if len(r.Question) == 0 {
			m.SetRcode(r, mdns.RcodeFormatError)
			_ = w.WriteMsg(m)
			return
		}
		q := r.Question[0]
		name := strings.ToLower(q.Name)
		if !strings.HasSuffix(name, zone) {
			m.SetRcode(r, mdns.RcodeRefused)
			_ = w.WriteMsg(m)
			return
		}
		m.SetReply(r)
		m.Authoritative = true
		header := mdns.RR_Header{Name: q.Name, Class: mdns.ClassINET, Ttl: 10}
		switch q.Qtype {
		case mdns.TypeA:
			header.Rrtype = mdns.TypeA
			m.Answer = append(m.Answer, &mdns.A{Hdr: header, A: net.IPv4(127, 0, 0, 1)})
		case mdns.TypeAAAA:
			header.Rrtype = mdns.TypeAAAA
			m.Answer = append(m.Answer, &mdns.AAAA{Hdr: header, AAAA: net.IPv6loopback})
		default:
			// Empty NOERROR: the name exists, the type has no records.
		}
		_ = w.WriteMsg(m)
	})

	// Bind explicitly first so port conflicts surface as errors here, not
	// in a goroutine. UDP is required; TCP is best-effort.
	pc, err := net.ListenPacket("udp", s.Addr)
	if err != nil {
		return fmt.Errorf("dns udp listen %s: %w", s.Addr, err)
	}
	s.udp = &mdns.Server{PacketConn: pc, Handler: handler}
	go func() { _ = s.udp.ActivateAndServe() }()

	if ln, err := net.Listen("tcp", pc.LocalAddr().String()); err != nil {
		s.TCPErr = fmt.Errorf("dns tcp listen: %w", err)
	} else {
		s.tcp = &mdns.Server{Listener: ln, Handler: handler}
		go func() { _ = s.tcp.ActivateAndServe() }()
	}
	return nil
}

// LocalAddr reports the bound UDP address (useful with :0 in tests).
func (s *Server) LocalAddr() string {
	if s.udp == nil || s.udp.PacketConn == nil {
		return ""
	}
	return s.udp.PacketConn.LocalAddr().String()
}

// Stop shuts both listeners down.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.udp != nil {
		_ = s.udp.ShutdownContext(ctx)
	}
	if s.tcp != nil {
		_ = s.tcp.ShutdownContext(ctx)
	}
}
