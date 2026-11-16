// SPDX-License-Identifier: MPL-2.0

package mux

import (
	"context"
	"net/http"
)

// Environment describes the access token in use and the environment it
// belongs to, as reported by the whoami endpoint.
type Environment struct {
	OrganizationID   string   `json:"organization_id"`
	OrganizationName string   `json:"organization_name"`
	EnvironmentID    string   `json:"environment_id"`
	EnvironmentName  string   `json:"environment_name"`
	EnvironmentType  string   `json:"environment_type"`
	AccessTokenName  string   `json:"access_token_name"`
	Permissions      []string `json:"permissions"`
}

// WhoAmI returns the token's environment. Every access token may call it,
// whatever its permissions.
func (c *Client) WhoAmI(ctx context.Context) (*Environment, error) {
	var out Environment
	if err := c.do(ctx, http.MethodGet, "/system/v1/whoami", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SigningKey is an RSA key pair for signing playback and data tokens. Mux
// keeps the public key.
type SigningKey struct {
	ID string `json:"id"`
	// CreatedAt is in seconds since the Unix epoch.
	CreatedAt string `json:"created_at"`
	// PrivateKey is the Base64-encoded PEM private key. Mux returns it only
	// when the key is created.
	PrivateKey string `json:"private_key"`
}

// CreateSigningKey generates a signing key. The result carries the private
// key, which cannot be retrieved again.
func (c *Client) CreateSigningKey(ctx context.Context) (*SigningKey, error) {
	var out SigningKey
	if err := c.do(ctx, http.MethodPost, "/system/v1/signing-keys", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSigningKey returns a signing key, without its private key. It looks the
// key up in the list of keys rather than by ID: Mux keeps answering a GET of
// a deleted key with 200 for minutes if the key was read before it was
// deleted, while the list drops it at once (observed 2026-11-16).
func (c *Client) GetSigningKey(ctx context.Context, id string) (*SigningKey, error) {
	keys, err := list[SigningKey](ctx, c, "/system/v1/signing-keys", pageSize)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Type: "not_found", Message: "Signing Key not found"}
}

// DeleteSigningKey deletes a signing key. Tokens signed with it stop working.
func (c *Client) DeleteSigningKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/system/v1/signing-keys/"+seg(id), nil, nil, nil)
}

// Webhook is an address Mux delivers event notifications to.
type Webhook struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Enabled bool   `json:"enabled"`
	// CreatedAt is an ISO 8601 UTC datetime.
	CreatedAt string `json:"created_at"`
	// SigningSecret verifies that a payload was sent by Mux. It is returned
	// only when the webhook is created.
	SigningSecret string `json:"signing_secret"`
}

// WebhookUpdate changes a webhook; nil fields are left as they are.
type WebhookUpdate struct {
	Address *string `json:"address,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// CreateWebhook creates a webhook, which starts out enabled. The address
// must be unique in the environment. The result carries the signing secret,
// which cannot be retrieved again.
func (c *Client) CreateWebhook(ctx context.Context, address string) (*Webhook, error) {
	var out Webhook
	body := map[string]string{"address": address}
	if err := c.do(ctx, http.MethodPost, "/system/v1/webhooks", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWebhook returns a webhook, without its signing secret.
func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, http.MethodGet, "/system/v1/webhooks/"+seg(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWebhook changes a webhook's address, whether it is enabled, or both.
func (c *Client) UpdateWebhook(ctx context.Context, id string, upd WebhookUpdate) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, http.MethodPatch, "/system/v1/webhooks/"+seg(id), nil, upd, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhook deletes a webhook and frees its address.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/system/v1/webhooks/"+seg(id), nil, nil, nil)
}
