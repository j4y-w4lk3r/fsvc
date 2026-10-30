// Package opvault reads the Revolut material stored in 1Password.
// fsvc cert generates a new keypair. It does not look at the vault.
// The existing pair and the API config live as three items in the
// x-fsvc vault, and this package copies them out through the op CLI.
package opvault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// DefaultVault is the 1Password vault that holds the Revolut
	// Business API material.
	DefaultVault = "x-fsvc"

	itemConfig = "fsvc revolut config"
	itemCert   = "rev bus api public cert"
	itemKey    = "rev bus api private key"
)

// Material is the on-disk Revolut setup, with secret values still in
// memory. Callers write it to disk and must not print the key or the
// refresh token.
type Material struct {
	BaseURL      string
	ClientID     string
	ISS          string
	RedirectURI  string
	RefreshToken string
	CertPEM      []byte
	KeyPEM       []byte
}

// Runner executes an op invocation and returns stdout.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Client reads one vault.
type Client struct {
	Vault string
	Run   Runner
}

// New returns a client that shells out to op on PATH.
func New(vault string) *Client {
	if vault == "" {
		vault = DefaultVault
	}
	return &Client{Vault: vault, Run: execOp}
}

// Fetch loads the config note and both PEM documents.
func (c *Client) Fetch(ctx context.Context) (*Material, error) {
	if c.Run == nil {
		c.Run = execOp
	}
	vault := c.Vault
	if vault == "" {
		vault = DefaultVault
	}

	note, err := c.Run(ctx, "item", "get", itemConfig, "--vault", vault, "--format", "json")
	if err != nil {
		return nil, err
	}
	fields, err := fieldValues(note)
	if err != nil {
		return nil, fmt.Errorf("opvault: parse %q: %w", itemConfig, err)
	}

	mat := &Material{
		BaseURL:      fields["base_url"],
		ClientID:     fields["client_id"],
		ISS:          fields["iss"],
		RedirectURI:  fields["redirect_uri"],
		RefreshToken: fields["refresh_token"],
	}
	for _, label := range []string{"base_url", "client_id", "iss", "redirect_uri"} {
		if strings.TrimSpace(fields[label]) == "" {
			return nil, fmt.Errorf("opvault: item %q is missing %s", itemConfig, label)
		}
	}

	cert, err := c.Run(ctx, "document", "get", itemCert, "--vault", vault)
	if err != nil {
		return nil, err
	}
	key, err := c.Run(ctx, "document", "get", itemKey, "--vault", vault)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(cert, []byte("BEGIN CERTIFICATE")) {
		return nil, fmt.Errorf("opvault: %q is not a certificate PEM", itemCert)
	}
	if !bytes.Contains(key, []byte("PRIVATE KEY")) {
		return nil, fmt.Errorf("opvault: %q is not a private key PEM", itemKey)
	}
	mat.CertPEM = cert
	mat.KeyPEM = key
	return mat, nil
}

func fieldValues(raw []byte) (map[string]string, error) {
	var item struct {
		Fields []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(item.Fields))
	for _, f := range item.Fields {
		if f.Label == "" {
			continue
		}
		out[f.Label] = f.Value
	}
	return out, nil
}

func execOp(ctx context.Context, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "op", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("op %s: %s", args[0], msg)
	}
	return stdout.Bytes(), nil
}
