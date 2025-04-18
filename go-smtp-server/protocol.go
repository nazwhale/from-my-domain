package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"
)

// ─── session state ────────────────────────────────────────────────────────────
// session represents the state of an SMTP conversation with a client
// It tracks the sender, recipients, message data, and current protocol step
type session struct {
	from   string   // Email address of the sender (MAIL FROM)
	rcpts  []string // List of recipient email addresses (RCPT TO)
	data   []string // Lines of the email message body
	step   string   // Current protocol step: "", "mail", "rcpt", "data"
	secure bool     // Whether the connection is using TLS encryption
}

// reset clears the session state but preserves the secure flag
// This is called after completing a message or when starting a new message
func (s *session) reset() { *s = session{secure: s.secure} }

// ─── helpers ──────────────────────────────────────────────────────────────────

// writeLine sends a line of text to the client with proper SMTP line endings (CRLF)
func writeLine(w *bufio.Writer, line string) {
	fmt.Fprintf(w, "%s\r\n", line)
	_ = w.Flush()
}

// parseCmd splits an SMTP command line into the command and its argument
// Commands are case-insensitive, so they're converted to uppercase
func parseCmd(line string) (cmd, arg string) {
	parts := strings.SplitN(line, " ", 2)
	cmd = strings.ToUpper(parts[0])
	if len(parts) == 2 {
		arg = parts[1]
	}
	return
}

// stripAddr removes the angle brackets from an email address
// e.g., "<user@example.com>" becomes "user@example.com"
func stripAddr(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "<")
	v = strings.TrimSuffix(v, ">")
	return v
}

// upgradeToTLS upgrades a connection to use TLS encryption
// Returns the upgraded connection, new reader and writer, or an error if the handshake fails
func upgradeToTLS(c net.Conn, tlsCfg *tls.Config, s *session) (net.Conn, *bufio.Scanner, *bufio.Writer, error) {
	// Upgrade the connection to TLS
	tlsConn := tls.Server(c, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		return nil, nil, nil, err
	}

	// Create new reader and writer for the secure connection
	r := bufio.NewScanner(tlsConn)
	w := bufio.NewWriter(tlsConn)

	// Update session state
	s.secure = true
	s.reset() // RFC requires discarding the prior SMTP state after STARTTLS

	return tlsConn, r, w, nil
}

// handleDATA processes the message content during the DATA phase
// It reads the message body until the end marker "." on a line by itself
// Returns the formatted message body and any error that occurred
func handleDATA(s *session, r *bufio.Scanner) (body string, err error) {
	// Read the message body until "." on a line by itself
	for r.Scan() {
		l := r.Text()
		if l == "." {
			break
		}
		s.data = append(s.data, l)
	}

	if err := r.Err(); err != nil {
		return "", err
	}

	// Format the message body with proper line endings
	body = strings.Join(s.data, "\r\n") + "\r\n"
	return body, nil
}

// ─── connection handler ───────────────────────────────────────────────────────

// handleConn processes an individual SMTP client connection
// It implements the core SMTP protocol handling logic
func handleConn(c net.Conn, tlsCfg *tls.Config) {
	defer c.Close()

	r := bufio.NewScanner(c)
	w := bufio.NewWriter(c)
	s := &session{}

	log.Printf("New connection from %s", c.RemoteAddr())
	// Send the initial greeting per SMTP protocol
	writeLine(w, "220 smtpmini ESMTP ready")

	for {
		// Read the next line from the client
		if !r.Scan() {
			if err := r.Err(); err != nil {
				log.Printf("conn error: %v", err)
			}
			log.Printf("Connection closed")
			return
		}
		cmd, arg := parseCmd(r.Text())
		log.Printf("Received command: %s %s", cmd, arg)

		// Process the command based on the SMTP protocol
		switch cmd {
		case "EHLO":
			// EHLO initiates the SMTP session and identifies the client
			// It also advertises server capabilities (extensions)
			if !s.secure {
				log.Printf("Sending EHLO response (insecure)")
				writeLine(w, "250-smtpmini")
				writeLine(w, "250-STARTTLS") // Advertise STARTTLS capability
				writeLine(w, "250 HELP")
			} else {
				log.Printf("Sending EHLO response (secure)")
				writeLine(w, "250 smtpmini")
			}

		case "HELO":
			// HELO is the older, simpler version of EHLO
			s.reset()
			log.Printf("Sending HELO response")
			writeLine(w, "250 Hello")

		case "STARTTLS":
			// STARTTLS command upgrades the connection to use TLS encryption
			if s.secure {
				log.Printf("Already in TLS mode")
				writeLine(w, "503 Already under TLS")
				continue
			}
			log.Printf("Starting TLS handshake")
			writeLine(w, "220 Ready to start TLS")

			// Upgrade the connection to TLS
			var err error
			c, r, w, err = upgradeToTLS(c, tlsCfg, s)
			if err != nil {
				log.Printf("TLS handshake failed: %v", err)
				return
			}

			log.Printf("TLS handshake successful")
			continue // Wait for client to issue EHLO again on the secure connection

		case "MAIL":
			// MAIL FROM command initiates a new message transaction
			// and specifies the sender's email address
			if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
				log.Printf("Invalid MAIL syntax")
				writeLine(w, "501 Syntax: MAIL FROM:<address>")
				continue
			}
			s.reset()
			s.from = stripAddr(arg[5:])
			s.step = "mail"
			log.Printf("MAIL FROM accepted: %s", s.from)
			writeLine(w, "250 OK")

		case "RCPT":
			// RCPT TO command specifies a recipient's email address
			// Multiple RCPT commands can be issued for multiple recipients
			if s.step == "" {
				log.Printf("RCPT without MAIL")
				writeLine(w, "503 Need MAIL FROM first")
				continue
			}
			if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
				log.Printf("Invalid RCPT syntax")
				writeLine(w, "501 Syntax: RCPT TO:<address>")
				continue
			}
			s.rcpts = append(s.rcpts, stripAddr(arg[3:]))
			s.step = "rcpt"
			log.Printf("RCPT TO accepted: %s", stripAddr(arg[3:]))
			writeLine(w, "250 OK")

		case "DATA":
			// DATA command begins the message content transmission
			if s.step != "rcpt" {
				log.Printf("DATA without RCPT")
				writeLine(w, "503 Need RCPT TO first")
				continue
			}
			log.Printf("Starting DATA phase")
			writeLine(w, "354 End with <CRLF>.<CRLF>")

			// Process the message body using the helper function
			body, err := handleDATA(s, r)
			if err != nil {
				log.Printf("Error reading DATA: %v", err)
				writeLine(w, "500 Error reading DATA")
				continue
			}

			// ── enqueue for delivery instead of just logging ───────────────────
			// serialises the mail into spool/…json.
			queueMessage(s.from, append([]string(nil), s.rcpts...), body)

			writeLine(w, "250 Queued")
			s.reset() // Reset session state for the next message

		case "QUIT":
			// QUIT command ends the SMTP session
			log.Printf("Client requested QUIT")
			writeLine(w, "221 Bye")
			return

		default:
			// Handle unknown or unsupported commands
			log.Printf("Unrecognized command: %s", cmd)
			writeLine(w, "500 Unrecognised command")
		}
	}
}
