package client

import (
	"context"
	"net/http"
)

type WebhookHeaderName struct {
	Name string `json:"name"`
}

type Webhook struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	URL            string              `json:"url"`
	Headers        []WebhookHeaderName `json:"headers"`
	Enabled        bool                `json:"enabled"`
	ProjectIDs     []string            `json:"projectIds"`
	EnvironmentIDs []string            `json:"environmentIds"`
	TagIDs         []string            `json:"tagIds"`
	EventTypes     []string            `json:"eventTypes"`
	AlertKinds     []string            `json:"alertKinds"`
	Secret         string              `json:"secret"`
	CreatedAt      string              `json:"createdAt"`
	UpdatedAt      string              `json:"updatedAt"`
}

type WebhookHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type WebhookRequest struct {
	Name           string          `json:"name"`
	URL            string          `json:"url"`
	Headers        []WebhookHeader `json:"headers"`
	Enabled        bool            `json:"enabled"`
	ProjectIDs     []string        `json:"projectIds"`
	EnvironmentIDs []string        `json:"environmentIds"`
	TagIDs         []string        `json:"tagIds"`
	EventTypes     []string        `json:"eventTypes"`
	AlertKinds     []string        `json:"alertKinds"`
}

func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out []Webhook
	if err := c.do(ctx, http.MethodGet, "/v1/webhooks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetWebhook(ctx context.Context, webhookID string) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, http.MethodGet, "/v1/webhooks/"+pathEscape(webhookID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateWebhook(ctx context.Context, req WebhookRequest) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, http.MethodPost, "/v1/webhooks", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateWebhook(ctx context.Context, webhookID string, req WebhookRequest) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, http.MethodPut, "/v1/webhooks/"+pathEscape(webhookID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteWebhook(ctx context.Context, webhookID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/webhooks/"+pathEscape(webhookID), nil, nil)
}
