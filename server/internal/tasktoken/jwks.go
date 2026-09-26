package tasktoken

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// JWK is one public key in a JWK Set (RFC 7517). Only public components are
// declared: the signing key's private half has no field to travel in.
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	Kid string `json:"kid,omitempty"`

	// EC (RFC 7518 §6.2)
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`

	// RSA (RFC 7518 §6.3)
	N string `json:"n,omitempty"`
	E string `json:"e,omitempty"`
}

// JWKSet is the document served at the JWKS endpoint.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// JWKS returns the public half of the signing key, once per distinct
// (kid, algorithm) pair used in the catalog.
//
// A deployment signs with a single private key, but templates may label it
// with different key ids. A verifier looks a key up by the "kid" in the token
// header, so every id in use needs its own entry — and a template with no
// key_id signs tokens with no kid, which needs an entry carrying none.
//
// A nil *Issuer means the feature is off and yields an empty set; the handler
// turns that into a 404 rather than serving an empty document.
func (i *Issuer) JWKS() (JWKSet, error) {
	if i == nil || i.catalog == nil {
		return JWKSet{Keys: []JWK{}}, nil
	}

	base, err := publicJWK(i.key)
	if err != nil {
		return JWKSet{}, err
	}

	set := JWKSet{Keys: []JWK{}}
	seen := map[[2]string]struct{}{}
	for _, tpl := range i.catalog.ordered {
		id := [2]string{tpl.KeyID, tpl.Algorithm}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		entry := base
		entry.Use = "sig"
		entry.Alg = tpl.Algorithm
		entry.Kid = tpl.KeyID
		set.Keys = append(set.Keys, entry)
	}
	return set, nil
}

// publicJWK renders the public half of a signing key. The key type is fixed
// by what parsePrivateKey accepts, so an unsupported type here is a bug
// rather than a configuration error.
func publicJWK(key any) (JWK, error) {
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		params := k.Curve.Params()
		crv, err := curveName(params.Name)
		if err != nil {
			return JWK{}, err
		}
		// Coordinates are fixed-width for the curve. big.Int.Bytes() drops
		// leading zero bytes, and a verifier that reads x/y into a
		// fixed-size buffer rejects a short one, so pad back to size.
		size := (params.BitSize + 7) / 8
		return JWK{
			Kty: "EC",
			Crv: crv,
			X:   base64URL(padLeft(k.X.Bytes(), size)),
			Y:   base64URL(padLeft(k.Y.Bytes(), size)),
		}, nil
	case *rsa.PrivateKey:
		e := make([]byte, 8)
		binary.BigEndian.PutUint64(e, uint64(k.E))
		return JWK{
			Kty: "RSA",
			N:   base64URL(k.N.Bytes()),
			E:   base64URL(trimLeadingZeros(e)),
		}, nil
	default:
		return JWK{}, fmt.Errorf("task token key: cannot publish a %T as a JWK", key)
	}
}

// curveName maps Go's curve names onto the JWK "crv" registry values.
func curveName(name string) (string, error) {
	switch name {
	case "P-256", "P-384", "P-521":
		return name, nil
	default:
		return "", fmt.Errorf("task token key: unsupported curve %q", name)
	}
}

func padLeft(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}

func trimLeadingZeros(b []byte) []byte {
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
