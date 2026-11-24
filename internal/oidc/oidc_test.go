package oidc_test

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"testing"

	"github.com/linkvt/hyperctl/internal/oidc"
)

func TestGenerateKeyReturnsRSAKeyWithPublicKey(t *testing.T) {
	privatePEM, err := oidc.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	publicPEM, err := oidc.PublicKey(privatePEM)
	if err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode(privatePEM)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatalf("private key is not a PEM RSA PRIVATE KEY block: %q", privatePEM)
	}
	private, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := private.N.BitLen(); got != 4096 {
		t.Errorf("key size: got %d bits, want 4096", got)
	}
	block, _ = pem.Decode(publicPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		t.Fatalf("public key is not a PEM PUBLIC KEY block: %q", publicPEM)
	}
	public, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !private.PublicKey.Equal(public.(*rsa.PublicKey)) {
		t.Error("public key does not belong to the private key")
	}
}

func TestDocumentsMatchCcoctl(t *testing.T) {
	publicPEM, err := os.ReadFile("testdata/ccoctl.public")
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "https://vlinkautonodeoidc.blob.core.windows.net/vlinkautonodeoidc"

	docs, err := oidc.Documents(issuer, publicPEM)
	if err != nil {
		t.Fatal(err)
	}

	var discovery map[string]any
	if err := json.Unmarshal(docs[".well-known/openid-configuration"], &discovery); err != nil {
		t.Fatalf("parsing discovery document: %v", err)
	}
	if discovery["issuer"] != issuer {
		t.Errorf("issuer: got %v", discovery["issuer"])
	}
	if want := issuer + "/openid/v1/jwks"; discovery["jwks_uri"] != want {
		t.Errorf("jwks_uri: got %v, want %s", discovery["jwks_uri"], want)
	}

	var got, want map[string]any
	if err := json.Unmarshal(docs["openid/v1/jwks"], &got); err != nil {
		t.Fatalf("parsing JWKS: %v", err)
	}
	wantJWKS, err := os.ReadFile("testdata/ccoctl.jwks")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantJWKS, &want); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("JWKS:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
	if len(docs) != 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}
}
