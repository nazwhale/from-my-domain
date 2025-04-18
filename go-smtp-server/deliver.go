// deliver.go
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ─── on‑disk message schema ───────────────────────────────────────────────────

type Message struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	Rcpts     []string  `json:"rcpts"`
	Data      string    `json:"data"` // full RFC‑822 string ending in \r\n
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	NextTry   time.Time `json:"next_try"`
}

// ─── queue helpers ───────────────────────────────────────────────────────────

const spoolDir = "spool"

func init() { _ = os.MkdirAll(spoolDir, 0o755) }

func enqueue(m *Message) error {
	m.ID = fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), strings.ReplaceAll(m.From, "@", "_"))
	m.NextTry = time.Now()
	f, err := os.Create(filepath.Join(spoolDir, m.ID))
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(m)
}

func loadQueued() ([]*Message, error) {
	var ms []*Message
	err := filepath.WalkDir(spoolDir, func(p string, d fs.DirEntry, _ error) error {
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		var m Message
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		ms = append(ms, &m)
		return nil
	})
	return ms, err
}

func remove(m *Message) { _ = os.Remove(filepath.Join(spoolDir, m.ID)) }
func persist(m *Message) {
	f, _ := os.Create(filepath.Join(spoolDir, m.ID))
	defer f.Close()
	_ = json.NewEncoder(f).Encode(m)
}

// ─── outbound SMTP ───────────────────────────────────────────────────────────

func deliver(m *Message) error {
	// 1) MX lookup
	domain := m.Rcpts[0][strings.LastIndexByte(m.Rcpts[0], '@')+1:]
	var host string
	if mx, err := net.LookupMX(domain); err == nil && len(mx) > 0 {
		host = mx[0].Host
	}
	if host == "" {
		return fmt.Errorf("no MX found for %s", domain)
	}
	addr := net.JoinHostPort(host, "25")

	// 2) connect
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	tp := textproto.NewConn(conn)

	read := func(expect int) error { _, _, err := tp.ReadResponse(expect); return err }

	if err := read(220); err != nil {
		return err
	}

	// 3) EHLO + STARTTLS detection / upgrade
	supportsTLS, err := ehlo(tp)
	if err != nil {
		return err
	}
	if supportsTLS {
		if err := tp.PrintfLine("STARTTLS"); err != nil {
			return err
		}
		if err := read(220); err != nil {
			return err
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, InsecureSkipVerify: true})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		tp = textproto.NewConn(tlsConn)
		if _, err = ehlo(tp); err != nil {
			return err
		}
	}

	// 4) envelope
	if err := tp.PrintfLine("MAIL FROM:<%s>", m.From); err != nil {
		return err
	}
	if err := read(250); err != nil {
		return err
	}
	for _, rcpt := range m.Rcpts {
		if err := tp.PrintfLine("RCPT TO:<%s>", rcpt); err != nil {
			return err
		}
		if err := read(250); err != nil {
			return err
		}
	}

	// 5) DATA
	if err := tp.PrintfLine("DATA"); err != nil {
		return err
	}
	if err := read(354); err != nil {
		return err
	}
	dotted := strings.ReplaceAll(m.Data, "\n.", "\n..")
	if err := tp.PrintfLine("%s\r\n.", dotted); err != nil {
		return err
	}
	if err := read(250); err != nil {
		return err
	}
	_ = tp.PrintfLine("QUIT")
	return nil
}

// ehlo sends EHLO and drains **all** 250‑ lines, returning whether STARTTLS
// was advertised.
func ehlo(tp *textproto.Conn) (supportsTLS bool, err error) {
	if err = tp.PrintfLine("EHLO smtpmini.local"); err != nil {
		return
	}
	for {
		line, e := tp.ReadLine()
		if e != nil {
			err = e
			return
		}
		if strings.HasPrefix(line, "250-") {
			if strings.Contains(line, "STARTTLS") {
				supportsTLS = true
			}
			continue
		}
		if strings.HasPrefix(line, "250 ") { // last line
			if strings.Contains(line, "STARTTLS") {
				supportsTLS = true
			}
			return
		}
		return false, fmt.Errorf("unexpected EHLO line: %s", line)
	}
}

// ─── retry scheduler ─────────────────────────────────────────────────────────

func launchScheduler() {
	go func() {
		for {
			msgs, _ := loadQueued()
			now := time.Now()
			for _, m := range msgs {
				if m.NextTry.After(now) {
					continue
				}
				if err := deliver(m); err != nil {
					m.Attempts++
					m.LastError = err.Error()
					m.NextTry = now.Add(time.Duration(m.Attempts*15) * time.Minute)
					persist(m)
					log.Printf("[queue] defer %s (%d tries): %v", m.ID, m.Attempts, err)
				} else {
					remove(m)
					log.Printf("[queue] delivered %s", m.ID)
				}
			}
			time.Sleep(1 * time.Minute)
		}
	}()
}

// ─── called from server side ─────────────────────────────────────────────────

func queueMessage(from string, rcpts []string, data string) {
	msg := &Message{From: from, Rcpts: rcpts, Data: data}
	if err := enqueue(msg); err != nil {
		log.Printf("[queue] enqueue error: %v", err)
	}
}
