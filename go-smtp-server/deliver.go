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

// Message is what we store on disk.
type Message struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	Rcpts     []string  `json:"rcpts"`
	Data      string    `json:"data"` // full RFC‑822 string \r\n terminated
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	NextTry   time.Time `json:"next_try"`
}

// ---------- persistent queue helpers ----------------------------------------

const spoolDir = "spool"

func init() { _ = os.MkdirAll(spoolDir, 0o755) }

func enqueue(m *Message) error {
	m.ID = fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), strings.ReplaceAll(m.From, "@", "_"))
	m.NextTry = time.Now()
	path := filepath.Join(spoolDir, m.ID)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(m)
}

// load all *.json files
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

// ---------- outbound SMTP delivery ------------------------------------------

func deliver(m *Message) error {
	// 1) MX lookup
	host := ""
	if mx, err := net.LookupMX(m.Rcpts[0][strings.LastIndex(m.Rcpts[0], "@")+1:]); err == nil && len(mx) > 0 {
		host = mx[0].Host
	}
	if host == "" {
		return fmt.Errorf("no MX found")
	}
	addr := net.JoinHostPort(host, "25")

	// 2) open connection & optional STARTTLS
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	text := textproto.NewConn(conn)
	defer text.Close()

	readCode := func(expected int) error {
		code, _, err := text.ReadResponse(expected)
		if err != nil || code != expected {
			return fmt.Errorf("want %d got %v %v", expected, code, err)
		}
		return nil
	}

	if err := readCode(220); err != nil {
		return err
	}
	_ = text.PrintfLine("EHLO smtpmini.local")
	line, _ := text.ReadLine()
	if !strings.HasPrefix(line, "250") {
		return fmt.Errorf("EHLO rejected")
	}
	if strings.Contains(line, "STARTTLS") { // multi‑line banners ignored for brevity
		_ = text.PrintfLine("STARTTLS")
		if err := readCode(220); err != nil {
			return err
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, InsecureSkipVerify: true})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		text = textproto.NewConn(tlsConn)
		_ = text.PrintfLine("EHLO smtpmini.local")
		if err := readCode(250); err != nil {
			return err
		}
	}

	// 3) envelope
	_ = text.PrintfLine("MAIL FROM:<%s>", m.From)
	if err := readCode(250); err != nil {
		return err
	}
	for _, r := range m.Rcpts {
		_ = text.PrintfLine("RCPT TO:<%s>", r)
		if err := readCode(250); err != nil {
			return err
		}
	}
	_ = text.PrintfLine("DATA")
	if err := readCode(354); err != nil {
		return err
	}
	// dot‑stuff body
	dotted := strings.ReplaceAll(m.Data, "\n.", "\n..")
	_ = text.PrintfLine("%s\r\n.", dotted)
	if err := readCode(250); err != nil {
		return err
	}
	_ = text.PrintfLine("QUIT")
	return nil
}

// ---------- background scheduler --------------------------------------------

func launchScheduler() {
	go func() {
		for {
			ms, _ := loadQueued()
			now := time.Now()
			for _, m := range ms {
				if m.NextTry.After(now) { // back‑off window
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

// ---------- helper for server side ------------------------------------------

func queueMessage(from string, rcpts []string, data string) {
	m := &Message{From: from, Rcpts: rcpts, Data: data}
	if err := enqueue(m); err != nil {
		log.Printf("[queue] enqueue error: %v", err)
	}
}
