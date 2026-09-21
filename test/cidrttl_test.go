package test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestCidrTTLRewriteAndPassthrough(t *testing.T) {
	dir := t.TempDir()
	table := filepath.Join(dir, "cidr.ttl")
	// Loopback source so the client address is bindable in sandboxes.
	if err := os.WriteFile(table, []byte("127.0.0.1/32 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	corefile := `.:0 {
	bind 127.0.0.1
	cidrttl ` + table + ` {
		reload 0
	}
	template IN A matched.example. {
		answer "{{ .Name }} 300 IN A 192.0.2.10"
	}`
	server, udp, _, err := CoreDNSServerAndPorts(corefile)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop()

	exchange := func(source string) *dns.Msg {
		t.Helper()
		remoteAddr, err := net.ResolveUDPAddr("udp4", udp)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.ParseIP(source).To4()}, remoteAddr)
		if err != nil {
			t.Fatalf("dial from %s: %v", source, err)
		}
		defer conn.Close()
		req := new(dns.Msg)
		req.SetQuestion("matched.example.", dns.TypeA)
		client := &dns.Client{Net: "udp4", Timeout: time.Second}
		resp, _, err := client.ExchangeWithConn(req, &dns.Conn{Conn: conn})
		if err != nil {
			t.Fatalf("exchange from %s: %v", source, err)
		}
		return resp
	}

	resp := exchange("127.0.0.1")
	if len(resp.Answer) != 1 || resp.Answer[0].Header().Ttl != 30 {
		t.Fatalf("expected one answer rewritten to TTL 30, got %v", resp.Answer)
	}
	if got := resp.Answer[0].(*dns.A).A.String(); got != "192.0.2.10" {
		t.Fatalf("answer content changed: %s", got)
	}

	// NXDOMAIN must stay empty.
	nx := new(dns.Msg)
	nx.SetQuestion("nonexistent.example.", dns.TypeA)
	nxConn, err := net.Dial("udp4", udp)
	if err != nil {
		t.Fatal(err)
	}
	defer nxConn.Close()
	nxClient := &dns.Client{Net: "udp4", Timeout: time.Second}
	nxResp, _, err := nxClient.ExchangeWithConn(nx, &dns.Conn{Conn: nxConn})
	if err != nil {
		t.Fatalf("nxdomain exchange: %v", err)
	}
	if len(nxResp.Answer) != 0 {
		t.Fatalf("answerless response must not gain records: %v", nxResp)
	}
}
