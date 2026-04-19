// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/choria-io/asyncjobs"
)

func TestHttpserver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTPServer")
}

// withJetStream boots an in-process NATS JetStream server and invokes cb with
// a client connection. Mirrors the pattern used by the top-level package.
func withJetStream(cb func(nc *nats.Conn)) {
	d, err := os.MkdirTemp("", "ajhttp")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(d)

	opts := &server.Options{
		JetStream: true,
		StoreDir:  d,
		Port:      -1,
		Host:      "localhost",
	}
	s, err := server.NewServer(opts)
	Expect(err).ToNot(HaveOccurred())
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		Fail("nats server did not start")
	}
	defer func() {
		s.Shutdown()
		s.WaitForShutdown()
	}()

	nc, err := nats.Connect(s.ClientURL(), nats.UseOldRequestStyle())
	Expect(err).ToNot(HaveOccurred())
	defer nc.Close()

	cb(nc)
}

func newTestClient(nc *nats.Conn) *asyncjobs.Client {
	client, err := asyncjobs.NewClient(asyncjobs.NatsConn(nc))
	Expect(err).ToNot(HaveOccurred())
	return client
}

// newTestServer builds a server with the given extra options. Callers supply
// only what they need; defaults are fine for most tests.
func newTestServer(client *asyncjobs.Client, opts ...Option) *Server {
	srv, err := NewServer(client, opts...)
	Expect(err).ToNot(HaveOccurred())
	return srv
}

// do performs a request against httptest.Server and decodes the body as text.
func do(ts *httptest.Server, method, path string, body io.Reader) (*http.Response, string) {
	req, err := http.NewRequest(method, ts.URL+path, body)
	Expect(err).ToNot(HaveOccurred())
	resp, err := ts.Client().Do(req)
	Expect(err).ToNot(HaveOccurred())
	b, err := io.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())
	resp.Body.Close()
	return resp, string(b)
}

var _ = Describe("Server", func() {
	BeforeEach(func() {
		log.SetOutput(GinkgoWriter)
	})

	Describe("NewServer", func() {
		It("requires a client", func() {
			_, err := NewServer(nil)
			Expect(err).To(MatchError(ContainSubstring("client is required")))
		})

		It("accepts default loopback bind without extra options", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("accepts [::1] loopback bind", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client, WithBindAddress("[::1]:0"))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("accepts localhost bind", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client, WithBindAddress("localhost:0"))
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("non-loopback bind guard", func() {
		It("refuses 0.0.0.0 without an acknowledgement", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client, WithBindAddress("0.0.0.0:0"))
				Expect(err).To(MatchError(ContainSubstring("non-loopback")))
				Expect(err).To(MatchError(ContainSubstring("WithUnauthenticatedExposure")))
			})
		})

		It("refuses a specific external IP without an acknowledgement", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client, WithBindAddress("192.0.2.5:0"))
				Expect(err).To(MatchError(ContainSubstring("non-loopback")))
			})
		})

		It("refuses wildcard IPv6 bind without an acknowledgement", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client, WithBindAddress("[::]:0"))
				Expect(err).To(MatchError(ContainSubstring("non-loopback")))
			})
		})

		It("accepts non-loopback with WithUnauthenticatedExposure", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client,
					WithBindAddress("0.0.0.0:0"),
					WithUnauthenticatedExposure(),
				)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("accepts non-loopback when mTLS is configured", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				dir := mustTempDir()
				defer os.RemoveAll(dir)

				certFile, keyFile, caFile := writeTestTLS(dir)

				_, err := NewServer(client,
					WithBindAddress("0.0.0.0:0"),
					WithTLS(certFile, keyFile),
					WithClientCA(caFile),
				)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("rejects WithClientCA without WithTLS", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				dir := mustTempDir()
				defer os.RemoveAll(dir)

				_, _, caFile := writeTestTLS(dir)

				_, err := NewServer(client, WithClientCA(caFile))
				Expect(err).To(MatchError(ContainSubstring("WithClientCA requires WithTLS")))
			})
		})

		It("rejects malformed bind addresses", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				_, err := NewServer(client, WithBindAddress("garbage"))
				Expect(err).To(HaveOccurred())
			})
		})

		It("rejects a CA file with no certificates", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				dir := mustTempDir()
				defer os.RemoveAll(dir)

				certFile, keyFile, _ := writeTestTLS(dir)
				emptyCA := filepath.Join(dir, "empty.pem")
				Expect(os.WriteFile(emptyCA, []byte("not a pem"), 0o600)).To(Succeed())

				_, err := NewServer(client,
					WithTLS(certFile, keyFile),
					WithClientCA(emptyCA),
				)
				Expect(err).To(MatchError(ContainSubstring("no certificates found")))
			})
		})
	})

	Describe("TLS config assembly", func() {
		It("builds a TLS config with mTLS when CA is provided", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				dir := mustTempDir()
				defer os.RemoveAll(dir)

				certFile, keyFile, caFile := writeTestTLS(dir)

				srv := newTestServer(client,
					WithTLS(certFile, keyFile),
					WithClientCA(caFile),
				)
				Expect(srv.tlsConfig).ToNot(BeNil())
				Expect(srv.tlsConfig.ClientAuth).To(Equal(tls.RequireAndVerifyClientCert))
				Expect(srv.tlsConfig.ClientCAs).ToNot(BeNil())
			})
		})

		It("leaves tlsConfig nil when neither TLS nor mTLS is configured", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)
				Expect(srv.tlsConfig).To(BeNil())
			})
		})
	})

	Describe("HTTP hardening", func() {
		It("rejects bodies larger than max_body_bytes", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client, WithMaxBodyBytes(32))

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				big := strings.NewReader(`{"name":"` + strings.Repeat("q", 1024) + `"}`)
				resp, _ := do(ts, http.MethodPost, "/v1/queues", big)
				Expect(resp.StatusCode).To(BeNumerically(">=", 400))
			})
		})
	})

	Describe("endpoints serve without an Authorization header", func() {
		It("answers /v1/info for any caller", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, body := do(ts, http.MethodGet, "/v1/info", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"version"`))
				Expect(body).To(ContainSubstring(`"auth":"none"`))
			})
		})

		It("creates a queue for any caller", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, _ := do(ts, http.MethodPost, "/v1/queues",
					strings.NewReader(`{"name":"open-queue"}`))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			})
		})
	})
})

func mustTempDir() string {
	dir, err := os.MkdirTemp("", "ajhttp-tls")
	Expect(err).ToNot(HaveOccurred())
	return dir
}

// writeTestTLS emits a self-signed certificate usable as both the server cert
// and a CA for the tests that need a client CA. Returns paths to the cert,
// the key, and the same cert (reusable as a CA trust anchor).
func writeTestTLS(dir string) (certFile, keyFile, caFile string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).ToNot(HaveOccurred())

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "asyncjobs-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).ToNot(HaveOccurred())

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	caFile = certFile

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	Expect(os.WriteFile(certFile, certPEM, 0o600)).To(Succeed())
	Expect(os.WriteFile(keyFile, keyPEM, 0o600)).To(Succeed())

	return certFile, keyFile, caFile
}
