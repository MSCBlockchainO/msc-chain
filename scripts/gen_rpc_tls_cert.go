package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

// main is the command entry point.
func main() {
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll("certs", 0700); err != nil {
		log.Fatal(err)
	}

	// `priv` and `err` store the error produced by this operation.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}

	// `serialNumber` and `err` store the error produced by this operation.
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Fatal(err)
	}

	// `tpl` stores the value produced by this operation.
	tpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-10 * time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	// `der` and `err` store the error produced by this operation.
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &priv.PublicKey, priv)
	if err != nil {
		log.Fatal(err)
	}

	// `certPath` stores the value produced by this operation.
	certPath := "certs/rpc.crt"
	// `keyPath` stores the key used to access the related value.
	keyPath := "certs/rpc.key"

	// `certOut` and `err` store the error produced by this operation.
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatal(err)
	}
	// `err` stores the error produced by this operation.
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		_ = certOut.Close()
		log.Fatal(err)
	}
	// `err` stores the error produced by this operation.
	if err := certOut.Close(); err != nil {
		log.Fatal(err)
	}

	// `keyOut` and `err` store the error produced by this operation.
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatal(err)
	}
	// `err` stores the error produced by this operation.
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		_ = keyOut.Close()
		log.Fatal(err)
	}
	// `err` stores the error produced by this operation.
	if err := keyOut.Close(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("generated %s and %s\n", certPath, keyPath)
}

