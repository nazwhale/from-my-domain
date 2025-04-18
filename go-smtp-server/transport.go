package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"time"
)

// ─── STARTTLS certificate (self‑signed if none on disk) ───────────────────────

// selfSignedCert generates a self-signed TLS certificate for STARTTLS support
// In a production environment, you would typically use a real certificate
func selfSignedCert() tls.Certificate {
	// Generate a 2048-bit RSA key pair
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	// Create a certificate template valid for one year
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "smtpmini"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}

	// Create a self-signed certificate from the template
	der, _ := x509.CreateCertificate(rand.Reader, templ, templ, &key.PublicKey, key)

	// Encode certificate and key to PEM format
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	// Create and return a TLS certificate
	cert, _ := tls.X509KeyPair(certPEM, keyPEM)
	return cert
}

// ─── public bootstrap API ─────────────────────────────────────────────────────

// Start launches the SMTP server on the specified address
// Returns a function to stop the server, the actual address it's listening on,
// and any error that occurred during startup
func Start(addr string) (stop func() error, actualAddr string, err error) {
	// Create a self-signed TLS certificate for STARTTLS support
	cert := selfSignedCert()
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	// Start listening on the specified TCP address
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}

	// Start accepting connections in a goroutine
	go func() {
		log.Printf("smtpmini listening on %s", ln.Addr())
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // Listener closed
			}
			// Handle each connection in its own goroutine
			go handleConn(conn, tlsCfg)
		}
	}()

	// Return a function to close the listener, the actual address, and nil error
	return ln.Close, ln.Addr().String(), nil
}
