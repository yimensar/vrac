package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// authenticator is a virtual platform authenticator: an ES256 key that
// answers WebAuthn ceremonies the way a browser does.
type authenticator struct {
	t      *testing.T
	key    *ecdsa.PrivateKey
	id     []byte
	handle []byte
	count  uint32
}

func newAuthenticator(t *testing.T) *authenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &authenticator{t: t, key: key, id: id}
}

var b64 = base64.RawURLEncoding

type publicKeyOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		RPID string `json:"rpId"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

func clientData(kind, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return b
}

func (a *authenticator) authData(rpID string, flags byte, attested []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	a.count++
	var buf bytes.Buffer
	buf.Write(h[:])
	buf.WriteByte(flags)
	binary.Write(&buf, binary.BigEndian, a.count)
	buf.Write(attested)
	return buf.Bytes()
}

// create answers navigator.credentials.create with a "none" attestation.
func (a *authenticator) create(options json.RawMessage, origin string) []byte {
	var o publicKeyOptions
	if err := json.Unmarshal(options, &o); err != nil {
		a.t.Fatal(err)
	}
	a.handle, _ = b64.DecodeString(o.PublicKey.User.ID)
	x, y := a.key.PublicKey.X.FillBytes(make([]byte, 32)), a.key.PublicKey.Y.FillBytes(make([]byte, 32))
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	var attested bytes.Buffer
	attested.Write(make([]byte, 16)) // AAGUID
	binary.Write(&attested, binary.BigEndian, uint16(len(a.id)))
	attested.Write(a.id)
	attested.Write(cose)
	auth := a.authData(o.PublicKey.RP.ID, 0x45, attested.Bytes()) // UP, UV, AT
	object, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": auth})
	body, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64.EncodeToString(clientData("webauthn.create", o.PublicKey.Challenge, origin)),
			"attestationObject": b64.EncodeToString(object)}})
	return body
}

// get answers navigator.credentials.get with a signed assertion.
func (a *authenticator) get(options json.RawMessage, origin string) []byte {
	var o publicKeyOptions
	if err := json.Unmarshal(options, &o); err != nil {
		a.t.Fatal(err)
	}
	cdj := clientData("webauthn.get", o.PublicKey.Challenge, origin)
	auth := a.authData(o.PublicKey.RPID, 0x05, nil) // UP, UV
	digest := sha256.Sum256(cdj)
	signed := sha256.Sum256(append(append([]byte{}, auth...), digest[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		a.t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64.EncodeToString(cdj), "authenticatorData": b64.EncodeToString(auth),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(a.handle)}})
	return body
}

type ceremonyReply struct {
	Flow    string          `json:"flow"`
	Mode    string          `json:"mode"`
	Options json.RawMessage `json:"options"`
}

// ceremony posts JSON and decodes a ceremony start.
func (h *harness) ceremony(path string, body any, want int) ceremonyReply {
	h.t.Helper()
	out := h.call("POST", path, body, want)
	var c ceremonyReply
	b, _ := json.Marshal(out)
	json.Unmarshal(b, &c)
	return c
}

// answer completes a ceremony with the authenticator.
func (h *harness) answer(a *authenticator, c ceremonyReply, finish string, want int) {
	h.t.Helper()
	var body []byte
	if c.Mode == "create" {
		body = a.create(c.Options, h.url)
	} else {
		body = a.get(c.Options, h.url)
	}
	req, _ := http.NewRequest("POST", h.url+finish+"?flow="+url.QueryEscape(c.Flow)+"&name=Test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(clientHeader, "web")
	res, err := h.web.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != want {
		h.t.Fatalf("finish = %d, want %d", res.StatusCode, want)
	}
}

const password = "correct horse battery"

// setup creates the owner with a passkey and returns the authenticator.
func (h *harness) setup() *authenticator {
	h.t.Helper()
	a := newAuthenticator(h.t)
	c := h.ceremony("/api/v1/setup/start", map[string]string{"username": "yan", "password": password}, 200)
	h.answer(a, c, "/api/v1/signin/finish", 200)
	return a
}

// signIn signs in with the password and the authenticator.
func (h *harness) signIn(a *authenticator) {
	h.t.Helper()
	c := h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": password}, 200)
	h.answer(a, c, "/api/v1/signin/finish", 200)
}
