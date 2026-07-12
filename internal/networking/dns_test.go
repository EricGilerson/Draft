package networking

import (
	"encoding/binary"
	"net"
	"testing"
)

func dnsQuery(name string, qtype uint16) []byte {
	q := []byte{0, 7, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range splitDNSName(name) {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	return append(q, 0, byte(qtype>>8), byte(qtype), 0, 1)
}

func splitDNSName(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			out = append(out, name[start:i])
			start = i + 1
		}
	}
	return out
}

func TestLocalDNSAnswersDraftA(t *testing.T) {
	reply := localDNSReply(dnsQuery("api.project.draft", 1))
	if got := binary.BigEndian.Uint16(reply[6:8]); got != 1 {
		t.Fatalf("answer count = %d, want 1", got)
	}
	if got := reply[len(reply)-4:]; string(got) != string([]byte{127, 0, 0, 1}) {
		t.Fatalf("A data = %v", got)
	}
}

func TestLocalDNSRejectsOtherDomains(t *testing.T) {
	reply := localDNSReply(dnsQuery("api.example.com", 1))
	if got := binary.BigEndian.Uint16(reply[2:4]) & 0xf; got != 3 {
		t.Fatalf("rcode = %d, want NXDOMAIN", got)
	}
}

func TestLocalDNSAnswersDraftAAAA(t *testing.T) {
	reply := localDNSReply(dnsQuery("api.project.draft", 28))
	if got := binary.BigEndian.Uint16(reply[6:8]); got != 1 {
		t.Fatalf("answer count = %d, want 1", got)
	}
	if len(reply) < 16 || reply[len(reply)-1] != 1 {
		t.Fatalf("AAAA data = %v", reply)
	}
}

func TestLocalDNSServesLoopbackOnly(t *testing.T) {
	var dns LocalDNS
	if err := dns.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer dns.Stop()

	conn, err := net.Dial("udp", dns.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(dnsQuery("api.project.draft", 1)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf[n-4 : n]; string(got) != string([]byte{127, 0, 0, 1}) {
		t.Fatalf("A data = %v", got)
	}
}
