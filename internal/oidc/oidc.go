// Package oidc creates the service account signing key of a hosted cluster
// and the OIDC discovery documents that let Azure trust its tokens.
package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
)

// keyBits matches the key size of ccoctl create-key-pair.
const keyBits = 4096

// Paths of the documents relative to the issuer URL.
const (
	DiscoveryPath = ".well-known/openid-configuration"
	JWKSPath      = "openid/v1/jwks"
)

// GenerateKey returns a PEM encoded RSA private key (PKCS #1), in the format
// of ccoctl create-key-pair.
func GenerateKey() ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), nil
}

// PublicKey returns the PEM encoded public key (PKIX) of a private key from
// GenerateKey.
func PublicKey(privatePEM []byte) ([]byte, error) {
	block, _ := pem.Decode(privatePEM)
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}), nil
}

type discoveryDocument struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
}

type jsonWebKeySet struct {
	Keys []jsonWebKey `json:"keys"`
}

type jsonWebKey struct {
	Use       string `json:"use"`
	KeyType   string `json:"kty"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

// Documents returns the OIDC discovery document and the JSON web key set of
// an issuer, keyed by their path relative to the issuer URL.
func Documents(issuerURL string, publicPEM []byte) (map[string][]byte, error) {
	block, _ := pem.Decode(publicPEM)
	if block == nil {
		return nil, errors.New("public key is not PEM encoded")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}
	public, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not an RSA key")
	}

	discovery, err := json.MarshalIndent(discoveryDocument{
		Issuer:                           issuerURL,
		JWKSURI:                          issuerURL + "/" + JWKSPath,
		ResponseTypesSupported:           []string{"id_token"},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{"RS256"},
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	jwks, err := json.MarshalIndent(jsonWebKeySet{Keys: []jsonWebKey{{
		Use:       "sig",
		KeyType:   "RSA",
		KeyID:     keyID(block.Bytes),
		Algorithm: "RS256",
		Modulus:   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
		Exponent:  base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
	}}}, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{DiscoveryPath: discovery, JWKSPath: jwks}, nil
}

// keyID derives the key ID from the DER encoded public key the way the
// kube-apiserver does for the tokens it signs.
func keyID(publicDER []byte) string {
	hash := sha256.Sum256(publicDER)
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
