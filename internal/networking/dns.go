package networking

// A deliberately small authoritative DNS responder for Draft's private
// *.draft namespace. It answers only loopback A/AAAA questions and never
// forwards DNS requests or listens on a network interface.

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

type LocalDNS struct {
	mu       sync.Mutex
	udp      net.PacketConn
	tcp      net.Listener
	addr     string
	shutdown chan struct{}
}

func (d *LocalDNS) Start(addr string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.udp != nil {
		return nil
	}
	udp, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("listen for local DNS on %s: %w", addr, err)
	}
	tcp, err := net.Listen("tcp", udp.LocalAddr().String())
	if err != nil {
		_ = udp.Close()
		return fmt.Errorf("listen for TCP local DNS on %s: %w", addr, err)
	}
	d.udp, d.tcp = udp, tcp
	d.addr = udp.LocalAddr().String()
	d.shutdown = make(chan struct{})
	go d.serveUDP(udp, d.shutdown)
	go d.serveTCP(tcp, d.shutdown)
	return nil
}

func (d *LocalDNS) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.udp == nil {
		return nil
	}
	close(d.shutdown)
	err := d.udp.Close()
	if tcpErr := d.tcp.Close(); err == nil {
		err = tcpErr
	}
	d.udp, d.tcp, d.addr, d.shutdown = nil, nil, "", nil
	return err
}

func (d *LocalDNS) Addr() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.addr
}

func (d *LocalDNS) serveUDP(conn net.PacketConn, done <-chan struct{}) {
	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-done:
				return
			default:
			}
			continue
		}
		if reply := localDNSReply(buf[:n]); len(reply) > 0 {
			_, _ = conn.WriteTo(reply, peer)
		}
	}
}

func (d *LocalDNS) serveTCP(listener net.Listener, done <-chan struct{}) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-done:
				return
			default:
			}
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			var size [2]byte
			if _, err := io.ReadFull(c, size[:]); err != nil {
				return
			}
			n := int(binary.BigEndian.Uint16(size[:]))
			if n < 12 || n > 65535 {
				return
			}
			query := make([]byte, n)
			if _, err := io.ReadFull(c, query); err != nil {
				return
			}
			reply := localDNSReply(query)
			if len(reply) == 0 {
				return
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(reply)))
			_, _ = c.Write(append(size[:], reply...))
		}(conn)
	}
}

func localDNSReply(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	qd := int(binary.BigEndian.Uint16(query[4:6]))
	if qd != 1 {
		return nil
	}
	name, end, ok := dnsName(query, 12)
	if !ok || end+4 > len(query) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(query[end : end+2])
	questionEnd := end + 4
	// Standard response, authoritative, recursion available, preserving RD.
	flags := uint16(0x8400) | (binary.BigEndian.Uint16(query[2:4]) & 0x0100)
	answer := []byte(nil)
	isDraftName := strings.HasSuffix(strings.TrimSuffix(name, "."), "."+LocalSuffix) || strings.TrimSuffix(name, ".") == LocalSuffix
	if isDraftName {
		switch qtype {
		case 1: // A
			answer = dnsAnswer(questionEnd, 1, []byte{127, 0, 0, 1})
		case 28: // AAAA
			answer = dnsAnswer(questionEnd, 28, net.ParseIP("::1").To16())
		}
	}
	// A name inside .draft with another record type gets NODATA, not NXDOMAIN,
	// so HTTPS/SVCB probing does not make browsers treat the hostname itself as
	// nonexistent. Names outside the namespace get NXDOMAIN.
	if answer == nil && !isDraftName {
		flags |= 0x0003
	}
	reply := make([]byte, 12, questionEnd+len(answer))
	copy(reply[:2], query[:2])
	binary.BigEndian.PutUint16(reply[2:4], flags)
	binary.BigEndian.PutUint16(reply[4:6], 1)
	if answer != nil {
		binary.BigEndian.PutUint16(reply[6:8], 1)
	}
	reply = append(reply, query[12:questionEnd]...)
	reply = append(reply, answer...)
	return reply
}

func dnsAnswer(questionEnd int, qtype uint16, data []byte) []byte {
	answer := make([]byte, 0, 16+len(data))
	answer = append(answer, 0xc0, 0x0c) // pointer to question name
	tmp := make([]byte, 10)
	binary.BigEndian.PutUint16(tmp[0:2], qtype)
	binary.BigEndian.PutUint16(tmp[2:4], 1) // IN
	binary.BigEndian.PutUint32(tmp[4:8], 30)
	binary.BigEndian.PutUint16(tmp[8:10], uint16(len(data)))
	return append(append(answer, tmp...), data...)
}

func dnsName(msg []byte, start int) (string, int, bool) {
	labels := []string{}
	for at := start; at < len(msg); {
		n := int(msg[at])
		at++
		if n == 0 {
			return strings.Join(labels, "."), at, true
		}
		if n > 63 || at+n > len(msg) {
			return "", 0, false
		}
		labels = append(labels, string(msg[at:at+n]))
		at += n
	}
	return "", 0, false
}
