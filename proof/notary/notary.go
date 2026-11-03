// Package notary stands in, for tests, for the outside services a Day's
// anchor comes from: three RFC 3161 timestamp authorities, each with its own
// root, and a transparency log that signs checkpoints, as local HTTP servers
// or called directly, with a Trust that pins them. Nothing here is a real
// authority or log; anchors it makes verify only under its own Trust.
package notary

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/digitorus/timestamp"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"

	"github.com/queone/mud-engine/proof"
)

// Notary is the three authorities and the log, with a key of its own for
// anchors it makes directly. Trust pins all of them.
type Notary struct {
	Trust   proof.Trust
	Key     *ecdsa.PrivateKey
	LogURL  string
	tsas    []authority
	signer  note.Signer
	logName string
	mu      sync.Mutex
	records [][]byte
	hashes  hashList
	down    map[string]bool
}

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

type hashList []tlog.Hash

func (h hashList) ReadHashes(indexes []int64) ([]tlog.Hash, error) {
	out := make([]tlog.Hash, len(indexes))
	for i, x := range indexes {
		if x < 0 || x >= int64(len(h)) {
			return nil, fmt.Errorf("no stored hash %d", x)
		}
		out[i] = h[x]
	}
	return out, nil
}

// New makes the authorities, the log, and the key, with a few entries already
// in the log so a proof has hashes to climb.
func New() (*Notary, error) {
	n := &Notary{LogURL: "https://log.test", logName: "log.test", down: map[string]bool{}}
	var err error
	if n.Key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		return nil, err
	}
	skey, vkey, err := note.GenerateKey(rand.Reader, n.logName)
	if err != nil {
		return nil, err
	}
	if n.signer, err = note.NewSigner(skey); err != nil {
		return nil, err
	}
	n.Trust.Logs = []proof.TrustedLog{{URL: n.LogURL, Name: n.logName, Key: vkey}}
	for _, name := range []string{"one", "two", "three"} {
		a, root, err := newAuthority(name)
		if err != nil {
			return nil, err
		}
		n.tsas = append(n.tsas, a)
		n.Trust.TSAs = append(n.Trust.TSAs, proof.TrustedTSA{Name: name, URL: "https://tsa-" + name + ".test", Root: root})
	}
	for i := range 5 {
		if err := n.append([]byte(fmt.Sprintf("someone else's entry %d", i))); err != nil {
			return nil, err
		}
	}
	return n, nil
}

// newAuthority makes a root and a timestamping certificate under it.
func newAuthority(name string) (authority, string, error) {
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return authority{}, "", err
	}
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	rootTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name + " root"}, NotBefore: start, NotAfter: start.AddDate(30, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		return authority{}, "", err
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return authority{}, "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return authority{}, "", err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: name + " tsa"}, NotBefore: start, NotAfter: start.AddDate(30, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, root, &key.PublicKey, rootKey)
	if err != nil {
		return authority{}, "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return authority{}, "", err
	}
	return authority{cert: cert, key: key}, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})), nil
}

// Token is authority i's TimeStampToken over digest at the given time.
func (n *Notary) Token(i int, digest []byte, at time.Time) ([]byte, error) {
	a := n.tsas[i]
	ts := timestamp.Timestamp{HashAlgorithm: crypto.SHA256, HashedMessage: digest, Time: at, Policy: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}, AddTSACertificate: true}
	resp, err := ts.CreateResponseWithOpts(a.cert, a.key, crypto.SHA256)
	if err != nil {
		return nil, err
	}
	parsed, err := timestamp.ParseResponse(resp)
	if err != nil {
		return nil, err
	}
	return parsed.RawToken, nil
}

// Response is authority i's full RFC 3161 response over digest, as its URL
// answers a request.
func (n *Notary) Response(i int, digest []byte, at time.Time) ([]byte, error) {
	a := n.tsas[i]
	ts := timestamp.Timestamp{HashAlgorithm: crypto.SHA256, HashedMessage: digest, Time: at, Policy: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}, AddTSACertificate: true}
	return ts.CreateResponseWithOpts(a.cert, a.key, crypto.SHA256)
}

// append adds one record to the log.
func (n *Notary) append(record []byte) error {
	hs, err := tlog.StoredHashes(int64(len(n.records)), record, n.hashes)
	if err != nil {
		return err
	}
	n.hashes = append(n.hashes, hs...)
	n.records = append(n.records, record)
	return nil
}

// Body is a hashedrekord 0.0.2 body for digest, signed by key.
func Body(digest []byte, key *ecdsa.PrivateKey) ([]byte, error) {
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest)
	if err != nil {
		return nil, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return bodyOf(digest, sig, base64.StdEncoding.EncodeToString(pub))
}

// bodyOf is the hashedrekord 0.0.2 body for a digest, its signature, and the
// base64 DER public key the signature verifies under.
func bodyOf(digest, sig []byte, pub string) ([]byte, error) {
	var b struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Spec       struct {
			HashedRekordV002 struct {
				Data struct {
					Algorithm string `json:"algorithm"`
					Digest    string `json:"digest"`
				} `json:"data"`
				Signature struct {
					Content  string `json:"content"`
					Verifier struct {
						KeyDetails string `json:"keyDetails"`
						PublicKey  struct {
							RawBytes string `json:"rawBytes"`
						} `json:"publicKey"`
					} `json:"verifier"`
				} `json:"signature"`
			} `json:"hashedRekordV002"`
		} `json:"spec"`
	}
	b.APIVersion, b.Kind = "0.0.2", "hashedrekord"
	h := &b.Spec.HashedRekordV002
	h.Data.Algorithm, h.Data.Digest = "SHA2_256", base64.StdEncoding.EncodeToString(digest)
	h.Signature.Content = base64.StdEncoding.EncodeToString(sig)
	h.Signature.Verifier.KeyDetails = "PKIX_ECDSA_P256_SHA_256"
	h.Signature.Verifier.PublicKey.RawBytes = pub
	return json.Marshal(b)
}

// Enter appends body to the log, then one more entry after it, and answers
// the log's TransparencyLogEntry for body, with its inclusion proof and a
// signed checkpoint, as Rekor v2 answers a new entry.
func (n *Notary) Enter(body []byte) (json.RawMessage, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	index := int64(len(n.records))
	if err := n.append(body); err != nil {
		return nil, err
	}
	if err := n.append([]byte("a later entry")); err != nil {
		return nil, err
	}
	size := int64(len(n.records))
	root, err := tlog.TreeHash(size, n.hashes)
	if err != nil {
		return nil, err
	}
	p, err := tlog.ProveRecord(size, index, n.hashes)
	if err != nil {
		return nil, err
	}
	text := fmt.Sprintf("%s\n%d\n%s\n", n.logName, size, base64.StdEncoding.EncodeToString(root[:]))
	envelope, err := note.Sign(&note.Note{Text: text}, n.signer)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(p))
	for i, h := range p {
		hashes[i] = base64.StdEncoding.EncodeToString(h[:])
	}
	var e struct {
		LogIndex       string `json:"logIndex"`
		InclusionProof struct {
			LogIndex   string   `json:"logIndex"`
			RootHash   string   `json:"rootHash"`
			TreeSize   string   `json:"treeSize"`
			Hashes     []string `json:"hashes"`
			Checkpoint struct {
				Envelope string `json:"envelope"`
			} `json:"checkpoint"`
		} `json:"inclusionProof"`
		CanonicalizedBody string `json:"canonicalizedBody"`
	}
	e.LogIndex = fmt.Sprint(index)
	e.InclusionProof.LogIndex = e.LogIndex
	e.InclusionProof.RootHash = base64.StdEncoding.EncodeToString(root[:])
	e.InclusionProof.TreeSize = fmt.Sprint(size)
	e.InclusionProof.Hashes = hashes
	e.InclusionProof.Checkpoint.Envelope = string(envelope)
	e.CanonicalizedBody = base64.StdEncoding.EncodeToString(body)
	return json.Marshal(e)
}

// Anchor anchors a statement: timestamps from the first two authorities, the
// later at the given time, and a log entry under the notary's key.
func (n *Notary) Anchor(statement []byte, at time.Time) (proof.Anchor, error) {
	sum := sha256.Sum256(statement)
	pub, err := x509.MarshalPKIXPublicKey(&n.Key.PublicKey)
	if err != nil {
		return proof.Anchor{}, err
	}
	a := proof.Anchor{Statement: string(statement), Digest: hex.EncodeToString(sum[:]), Key: base64.StdEncoding.EncodeToString(pub), Time: at.UTC()}
	for i, t := range []time.Time{at.Add(-time.Second), at} {
		token, err := n.Token(i, sum[:], t)
		if err != nil {
			return proof.Anchor{}, err
		}
		a.Timestamps = append(a.Timestamps, proof.Timestamp{TSA: n.Trust.TSAs[i].URL, Token: base64.StdEncoding.EncodeToString(token), Time: t.UTC()})
	}
	body, err := Body(sum[:], n.Key)
	if err != nil {
		return proof.Anchor{}, err
	}
	entry, err := n.Enter(body)
	if err != nil {
		return proof.Anchor{}, err
	}
	a.Log = proof.LogEntry{URL: n.LogURL, Entry: entry}
	return a, nil
}

// SetDown takes one service down or brings it back: an authority by its
// name, "one", "two", or "three", or the log as "log". A service that is down
// answers 503.
func (n *Notary) SetDown(name string, down bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.down[name] = down
}

func (n *Notary) isDown(name string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.down[name]
}

// Serve starts the authorities and the log as local HTTP servers and points
// Trust at them. The authorities sign at the time now gives. Stop closes them.
func (n *Notary) Serve(now func() time.Time) (stop func()) {
	var servers []*httptest.Server
	for i := range n.tsas {
		srv := httptest.NewServer(n.tsaHandler(i, now))
		servers = append(servers, srv)
		n.Trust.TSAs[i].URL = srv.URL
	}
	srv := httptest.NewServer(n.logHandler())
	servers = append(servers, srv)
	n.LogURL = srv.URL
	n.Trust.Logs[0].URL = srv.URL
	return func() {
		for _, s := range servers {
			s.Close()
		}
	}
}

// tsaHandler answers RFC 3161 requests as authority i.
func (n *Notary) tsaHandler(i int, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.isDown(n.Trust.TSAs[i].Name) {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req, err := timestamp.ParseRequest(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := n.Response(i, req.HashedMessage, now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(resp)
	})
}

// logHandler answers Rekor v2 entry requests at /api/v2/log/entries.
func (n *Notary) logHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/log/entries", func(w http.ResponseWriter, r *http.Request) {
		if n.isDown("log") {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			HashedRekordRequestV002 struct {
				Digest    string `json:"digest"`
				Signature struct {
					Content  string `json:"content"`
					Verifier struct {
						PublicKey struct {
							RawBytes string `json:"rawBytes"`
						} `json:"publicKey"`
						KeyDetails string `json:"keyDetails"`
					} `json:"verifier"`
				} `json:"signature"`
			} `json:"hashedRekordRequestV002"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h := req.HashedRekordRequestV002
		digest, err1 := base64.StdEncoding.DecodeString(h.Digest)
		sig, err2 := base64.StdEncoding.DecodeString(h.Signature.Content)
		pub, err3 := proof.ParseAnchorKey(h.Signature.Verifier.PublicKey.RawBytes)
		if err1 != nil || err2 != nil || err3 != nil || !ecdsa.VerifyASN1(pub, digest, sig) {
			http.Error(w, "the entry's signature does not verify", http.StatusBadRequest)
			return
		}
		body, err := bodyOf(digest, sig, h.Signature.Verifier.PublicKey.RawBytes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entry, err := n.Enter(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(bytes.TrimSpace(entry))
	})
	return mux
}
