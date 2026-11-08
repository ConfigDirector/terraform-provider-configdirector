package client

import (
	"context"
	"net/http"
)

type Integration struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Kind           string             `json:"kind"`
	Settings       map[string]*string `json:"settings"`
	Enabled        bool               `json:"enabled"`
	ProjectIDs     []string           `json:"projectIds"`
	EnvironmentIDs []string           `json:"environmentIds"`
	TagIDs         []string           `json:"tagIds"`
	CreatedAt      string             `json:"createdAt"`
	UpdatedAt      string             `json:"updatedAt"`
}

type IntegrationRequest struct {
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	Settings       map[string]string `json:"settings"`
	Enabled        bool              `json:"enabled"`
	ProjectIDs     []string          `json:"projectIds"`
	EnvironmentIDs []string          `json:"environmentIds"`
	TagIDs         []string          `json:"tagIds"`
}

func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {
	var out []Integration
	if err := c.do(ctx, http.MethodGet, "/v1/integrations", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetIntegration(ctx context.Context, integrationID string) (*Integration, error) {
	var out Integration
	if err := c.do(ctx, http.MethodGet, "/v1/integrations/"+pathEscape(integrationID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateIntegration(ctx context.Context, req IntegrationRequest) (*Integration, error) {
	var out Integration
	if err := c.do(ctx, http.MethodPost, "/v1/integrations", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateIntegration(ctx context.Context, integrationID string, req IntegrationRequest) (*Integration, error) {
	var out Integration
	if err := c.do(ctx, http.MethodPut, "/v1/integrations/"+pathEscape(integrationID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteIntegration(ctx context.Context, integrationID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/integrations/"+pathEscape(integrationID), nil, nil)
}
