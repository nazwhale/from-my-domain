package main

import (
	"bufio"
	"crypto/tls"
	"net"
	"strings"
	"testing"
)

func readLine(r *bufio.Reader, t *testing.T) string {
	l, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.TrimSpace(l)
}
func expect(t *testing.T, got, wantPrefix string) {
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("want %q, got %q", wantPrefix, got)
	}
}

func TestStartTLSConversation(t *testing.T) {
	stop, addr, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = stop() })

	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer raw.Close()
	r := bufio.NewReader(raw)
	expect(t, readLine(r, t), "220")

	// EHLO ------------------------------------------------------------------
	_, _ = raw.Write([]byte("EHLO client\r\n"))

	for {
		line := readLine(r, t)
		if strings.HasPrefix(line, "250-") {
			// expecting at least STARTTLS in one of the dash lines
			if strings.Contains(line, "STARTTLS") {
				// noted, keep looping
			}
			continue
		}
		// line starts with "250 " → final line
		break
	}

	// STARTTLS handshake ----------------------------------------------------
	_, _ = raw.Write([]byte("STARTTLS\r\n"))
	expect(t, readLine(r, t), "220")

	tlsConn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake: %v", err)
	}
	rTLS := bufio.NewReader(tlsConn)

	_, _ = tlsConn.Write([]byte("EHLO client\r\n"))
	expect(t, readLine(rTLS, t), "250 smtpmini")

	// rest of pipeline ------------------------------------------------------
	_, _ = tlsConn.Write([]byte("MAIL FROM:<a@b>\r\n"))
	expect(t, readLine(rTLS, t), "250")
	_, _ = tlsConn.Write([]byte("RCPT TO:<c@d>\r\n"))
	expect(t, readLine(rTLS, t), "250")
	_, _ = tlsConn.Write([]byte("DATA\r\n"))
	expect(t, readLine(rTLS, t), "354")
	_, _ = tlsConn.Write([]byte("Subject: hi\r\n\r\nbody\r\n.\r\n"))
	expect(t, readLine(rTLS, t), "250")
	_, _ = tlsConn.Write([]byte("QUIT\r\n"))
	expect(t, readLine(rTLS, t), "221")
}
