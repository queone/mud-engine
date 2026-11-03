package proof

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/digitorus/timestamp"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

// Anchor is the evidence, kept outside the site, that a Day's commitment
// existed before its round: timestamps from independent RFC 3161 timestamp
// authorities over the statement's digest, and an entry for the same digest
// in a public transparency log, signed by the site's anchor key.
type Anchor struct {
	Statement  string      `json:"statement"`  // compact JSON the digest hashes
	Digest     string      `json:"digest"`     // hex SHA-256 of Statement
	Key        string      `json:"key"`        // base64 DER of the site's ECDSA P-256 anchor key
	Timestamps []Timestamp `json:"timestamps"` // one per authority
	Log        LogEntry    `json:"log"`
	Time       time.Time   `json:"time"` // the later timestamp's time
}

// Timestamp is one authority's signed time over the digest.
type Timestamp struct {
	TSA   string    `json:"tsa"`   // the authority's URL
	Token string    `json:"token"` // base64 DER of its TimeStampToken
	Time  time.Time `json:"time"`
}

// LogEntry is the transparency log's answer to the site's entry: the log's
// URL and its TransparencyLogEntry, with the inclusion proof and checkpoint.
type LogEntry struct {
	URL   string          `json:"url"`
	Entry json.RawMessage `json:"entry"`
}

// Statement is what a Day's anchor stands for: the circuit's public seed,
// the season, the Day, and the Day's inputs hash, as compact JSON.
func Statement(circuitSeed uint64, season uint32, day int, inputsHash string) []byte {
	raw, _ := json.Marshal(struct {
		CircuitSeed uint64 `json:"circuit_seed"`
		Season      uint32 `json:"season"`
		Day         int    `json:"day"`
		InputsHash  string `json:"inputs_hash"`
	}{circuitSeed, season, day, inputsHash})
	return raw
}

// VerifyAnchor checks an anchor against the Day it names: the statement and
// its digest, timestamps from at least two pinned authorities, and the log
// entry. It answers the later timestamp's time, before which the commitment
// existed.
func VerifyAnchor(a Anchor, circuitSeed uint64, season uint32, day int, inputsHash string, trust Trust) (time.Time, error) {
	want := Statement(circuitSeed, season, day, inputsHash)
	if a.Statement != string(want) {
		return time.Time{}, fmt.Errorf("the anchor's statement is %s, not this Day's %s", a.Statement, want)
	}
	sum := sha256.Sum256(want)
	if a.Digest != hex.EncodeToString(sum[:]) {
		return time.Time{}, fmt.Errorf("the anchor's digest %s is not its statement's SHA-256 %x", a.Digest, sum)
	}
	var at time.Time
	seen := map[string]bool{}
	for _, ts := range a.Timestamps {
		der, err := base64.StdEncoding.DecodeString(ts.Token)
		if err != nil {
			return time.Time{}, fmt.Errorf("the timestamp from %s is not base64: %w", ts.TSA, err)
		}
		name, t, err := VerifyToken(der, sum[:], trust)
		if err != nil {
			return time.Time{}, fmt.Errorf("the timestamp from %s: %w", ts.TSA, err)
		}
		if seen[name] {
			return time.Time{}, fmt.Errorf("two timestamps come from %s; each authority counts once", name)
		}
		seen[name] = true
		if t.After(at) {
			at = t
		}
	}
	if len(seen) < 2 {
		return time.Time{}, fmt.Errorf("the anchor has timestamps from %d authorities; two are needed", len(seen))
	}
	if !a.Time.Equal(at) {
		return time.Time{}, fmt.Errorf("the anchor names time %s, but its later timestamp is %s", a.Time.UTC().Format(time.RFC3339), at.Format(time.RFC3339))
	}
	if _, err := VerifyLogEntry(a.Log, sum[:], a.Key, trust); err != nil {
		return time.Time{}, err
	}
	return at, nil
}

// VerifyToken checks one RFC 3161 TimeStampToken: its signature, a SHA-256
// imprint of digest, and a signer that chains to a pinned authority's root
// at the token's own time. It answers the authority's name and the time.
func VerifyToken(token, digest []byte, trust Trust) (string, time.Time, error) {
	ts, err := timestamp.Parse(token)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("the token does not parse or its signature fails: %w", err)
	}
	if !ts.AddTSACertificate {
		return "", time.Time{}, errors.New("the token carries no certificate to check its signer by")
	}
	if ts.HashAlgorithm != crypto.SHA256 || !bytes.Equal(ts.HashedMessage, digest) {
		return "", time.Time{}, fmt.Errorf("the token is over %x, not the digest %x", ts.HashedMessage, digest)
	}
	p7, err := pkcs7.Parse(token)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("the token's signed data does not parse: %w", err)
	}
	signer := p7.GetOnlySigner()
	if signer == nil {
		return "", time.Time{}, errors.New("the token has no single signer")
	}
	intermediates := x509.NewCertPool()
	for _, c := range p7.Certificates {
		intermediates.AddCert(c)
	}
	for _, t := range trust.TSAs {
		block, _ := pem.Decode([]byte(t.Root))
		if block == nil {
			continue
		}
		root, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		roots := x509.NewCertPool()
		roots.AddCert(root)
		opts := x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: ts.Time, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}}
		if _, err := signer.Verify(opts); err == nil {
			return t.Name, ts.Time.UTC(), nil
		}
	}
	return "", time.Time{}, fmt.Errorf("its signer %q chains to no pinned authority's root", signer.Subject.CommonName)
}

// transparencyLogEntry is the part of a Rekor v2 TransparencyLogEntry the
// check reads.
type transparencyLogEntry struct {
	LogIndex       string `json:"logIndex"`
	InclusionProof struct {
		LogIndex   string   `json:"logIndex"`
		Hashes     []string `json:"hashes"`
		Checkpoint struct {
			Envelope string `json:"envelope"`
		} `json:"checkpoint"`
	} `json:"inclusionProof"`
	CanonicalizedBody string `json:"canonicalizedBody"`
}

// hashedRekord is a log entry's body, a hashedrekord v0.0.2.
type hashedRekord struct {
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

// VerifyLogEntry checks a log entry: a pinned log, a hashedrekord body for
// digest under key whose ECDSA signature verifies, and the body's inclusion
// at its index in a checkpoint the log signed. It answers the index.
func VerifyLogEntry(e LogEntry, digest []byte, key string, trust Trust) (int64, error) {
	var log *TrustedLog
	for i := range trust.Logs {
		if trust.Logs[i].URL == e.URL {
			log = &trust.Logs[i]
		}
	}
	if log == nil {
		return 0, fmt.Errorf("the log %s is not a pinned log", e.URL)
	}
	var entry transparencyLogEntry
	if err := json.Unmarshal(e.Entry, &entry); err != nil {
		return 0, fmt.Errorf("the log entry is not the expected JSON: %w", err)
	}
	body, err := base64.StdEncoding.DecodeString(entry.CanonicalizedBody)
	if err != nil {
		return 0, fmt.Errorf("the log entry's body is not base64: %w", err)
	}
	var rekord hashedRekord
	if err := json.Unmarshal(body, &rekord); err != nil {
		return 0, fmt.Errorf("the log entry's body is not the expected JSON: %w", err)
	}
	spec := rekord.Spec.HashedRekordV002
	switch {
	case rekord.Kind != "hashedrekord" || rekord.APIVersion != "0.0.2":
		return 0, fmt.Errorf("the log entry is a %s %s, not a hashedrekord 0.0.2", rekord.Kind, rekord.APIVersion)
	case spec.Data.Algorithm != "SHA2_256" || spec.Data.Digest != base64.StdEncoding.EncodeToString(digest):
		return 0, fmt.Errorf("the log entry records %s %s, not the anchor's digest", spec.Data.Algorithm, spec.Data.Digest)
	case spec.Signature.Verifier.KeyDetails != "PKIX_ECDSA_P256_SHA_256" || spec.Signature.Verifier.PublicKey.RawBytes != key:
		return 0, errors.New("the log entry is signed under another key than the anchor's")
	}
	pub, err := ParseAnchorKey(key)
	if err != nil {
		return 0, err
	}
	sig, err := base64.StdEncoding.DecodeString(spec.Signature.Content)
	if err != nil || !ecdsa.VerifyASN1(pub, digest, sig) {
		return 0, errors.New("the log entry's signature does not verify under the anchor key")
	}
	verifier, err := note.NewVerifier(log.Key)
	if err != nil {
		return 0, fmt.Errorf("the pinned key of %s: %w", log.Name, err)
	}
	n, err := note.Open([]byte(entry.InclusionProof.Checkpoint.Envelope), note.VerifierList(verifier))
	if err != nil {
		return 0, fmt.Errorf("the log's checkpoint is not signed by %s: %w", log.Name, err)
	}
	lines := strings.Split(n.Text, "\n")
	if len(lines) < 3 || lines[0] != log.Name {
		return 0, fmt.Errorf("the checkpoint is not one of %s", log.Name)
	}
	size, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the checkpoint's tree size %q: %w", lines[1], err)
	}
	rootRaw, err := base64.StdEncoding.DecodeString(lines[2])
	if err != nil || len(rootRaw) != tlog.HashSize {
		return 0, fmt.Errorf("the checkpoint's root hash %q is not a SHA-256 hash", lines[2])
	}
	var root tlog.Hash
	copy(root[:], rootRaw)
	index, err := strconv.ParseInt(entry.LogIndex, 10, 64)
	if err != nil || entry.InclusionProof.LogIndex != entry.LogIndex {
		return 0, fmt.Errorf("the log entry's index %q and its proof's %q disagree", entry.LogIndex, entry.InclusionProof.LogIndex)
	}
	proof := make(tlog.RecordProof, len(entry.InclusionProof.Hashes))
	for i, h := range entry.InclusionProof.Hashes {
		raw, err := base64.StdEncoding.DecodeString(h)
		if err != nil || len(raw) != tlog.HashSize {
			return 0, fmt.Errorf("the inclusion proof's hash %d is not a SHA-256 hash", i)
		}
		copy(proof[i][:], raw)
	}
	if err := tlog.CheckRecord(proof, size, root, index, tlog.RecordHash(body)); err != nil {
		return 0, fmt.Errorf("the log entry %d is not in %s's checkpoint of %d entries: %w", index, log.Name, size, err)
	}
	return index, nil
}

// ParseAnchorKey reads an anchor key, base64 DER of an ECDSA P-256 public key.
func ParseAnchorKey(key string) (*ecdsa.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("the anchor key is not base64: %w", err)
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("the anchor key does not parse: %w", err)
	}
	pub, ok := k.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("the anchor key is not an ECDSA P-256 key")
	}
	return pub, nil
}
