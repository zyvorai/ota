// SPDX-License-Identifier: Apache-2.0
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zyvorai/ota/internal/fleetref"
	"github.com/zyvorai/ota/internal/ota"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8443", "HTTPS listen address")
	token := flag.String("token", "", "shared bearer token for registered devices (required)")
	devices := flag.String("devices", "", "comma-separated device IDs to authorize")
	assignmentFile := flag.String("assignment", "", "optional Assignment JSON delivered to all registered devices")
	certFile := flag.String("cert", "", "TLS certificate PEM (optional; ephemeral cert if empty)")
	keyFile := flag.String("key", "", "TLS private key PEM (optional)")
	writeCA := flag.String("write-ca", "", "write the server certificate PEM (lab)")
	flag.Parse()
	if *token == "" || *devices == "" {
		log.Fatal("usage: zyvor-fleet-ref -token SECRET -devices id1,id2 [-assignment file.json] [-listen addr]")
	}

	ref := fleetref.New()
	for _, id := range strings.Split(*devices, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := ref.RegisterDevice(id, *token); err != nil {
			log.Fatal(err)
		}
	}
	if *assignmentFile != "" {
		b, err := os.ReadFile(*assignmentFile)
		if err != nil {
			log.Fatal(err)
		}
		var a ota.Assignment
		if err := ota.StrictJSON(b, &a); err != nil {
			log.Fatal(err)
		}
		for _, id := range strings.Split(*devices, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			cp := a
			cp.DeviceID = id
			ref.SetAssignment(id, &cp)
		}
	}

	tlsCert, certPEM, err := loadOrEphemeralCert(*certFile, *keyFile)
	if err != nil {
		log.Fatal(err)
	}
	if *writeCA != "" {
		if err = os.WriteFile(*writeCA, certPEM, 0644); err != nil {
			log.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Handler:   ref.Handler(),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12},
	}
	fmt.Fprintf(os.Stderr, "zyvor-fleet-ref listening on https://%s (devices=%s)\n", ln.Addr().String(), *devices)
	log.Fatal(server.ServeTLS(ln, "", ""))
}

func loadOrEphemeralCert(certFile, keyFile string) (tls.Certificate, []byte, error) {
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return tls.Certificate{}, nil, fmt.Errorf("both -cert and -key are required together")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return tls.Certificate{}, nil, err
		}
		pemBytes, err := os.ReadFile(certFile)
		return cert, pemBytes, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "zyvor-fleet-ref-lab"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	return cert, certPEM, err
}
