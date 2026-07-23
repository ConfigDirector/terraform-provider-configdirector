package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		HTTPClient: http.DefaultClient,
	}
}

// APIError represents a non-2xx response from the ConfigDirector API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("configdirector API returned status %d: %s", e.StatusCode, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decoding response body: %w", err)
		}
	}

	return nil
}

func pathEscape(s string) string {
	return url.PathEscape(s)
}

// --- Projects ---

type Project struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Slug           string        `json:"slug"`
	OrganizationID string        `json:"organizationId"`
	CreatedAt      string        `json:"createdAt"`
	UpdatedAt      string        `json:"updatedAt"`
	Environments   []Environment `json:"environments,omitempty"`
}

type CreateProjectRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPost, "/projects", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetProject(ctx context.Context, projectID string) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodGet, "/projects/"+pathEscape(projectID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out []Project
	if err := c.do(ctx, http.MethodGet, "/projects", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteProject(ctx context.Context, projectID string) error {
	return c.do(ctx, http.MethodDelete, "/projects/"+pathEscape(projectID), nil, nil)
}

// --- Environments ---

type Environment struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	ProjectID string `json:"projectId"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Live      bool   `json:"live"`
}

type CreateEnvironmentRequest struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color"`
	Live  bool   `json:"live"`
}

type UpdateEnvironmentRequest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color"`
	Live  bool   `json:"live"`
}

func (c *Client) CreateEnvironment(ctx context.Context, projectID string, req CreateEnvironmentRequest) (*Environment, error) {
	var out Environment
	path := fmt.Sprintf("/projects/%s/environments", pathEscape(projectID))
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEnvironment(ctx context.Context, projectID, environmentID string) (*Environment, error) {
	var out Environment
	path := fmt.Sprintf("/projects/%s/environments/%s", pathEscape(projectID), pathEscape(environmentID))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateEnvironment(ctx context.Context, projectID, environmentID string, req UpdateEnvironmentRequest) (*Environment, error) {
	var out Environment
	path := fmt.Sprintf("/projects/%s/environments/%s", pathEscape(projectID), pathEscape(environmentID))
	if err := c.do(ctx, http.MethodPut, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListEnvironments(ctx context.Context, projectID string) ([]Environment, error) {
	var out []Environment
	path := fmt.Sprintf("/projects/%s/environments", pathEscape(projectID))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteEnvironment(ctx context.Context, projectID, environmentID string) error {
	path := fmt.Sprintf("/projects/%s/environments/%s", pathEscape(projectID), pathEscape(environmentID))
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// --- Configs ---

type DeprecatedKey struct {
	ID        string `json:"id"`
	ConfigID  string `json:"configId"`
	Key       string `json:"key"`
	IsPrimary bool   `json:"isPrimary"`
}

// Config is the API representation of a config. defaultValue, typeOptions,
// variations and targets are intentionally not modeled here: they are
// polymorphic/union-typed fields in the OpenAPI spec that the Terraform
// schema codegen cannot represent, so the resource treats them as
// write-only, JSON-encoded strings (see ConfigModel in the provider package).
type Config struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	Key            string          `json:"key"`
	Description    *string         `json:"description"`
	Role           string          `json:"role"`
	Lifetime       string          `json:"lifetime"`
	Type           string          `json:"type"`
	State          string          `json:"state"`
	Client         bool            `json:"client"`
	Server         bool            `json:"server"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
	DeprecatedKeys []DeprecatedKey `json:"deprecatedKeys"`
}

type CreateConfigRequest struct {
	Key          string  `json:"key"`
	Description  *string `json:"description,omitempty"`
	Role         string  `json:"role"`
	Lifetime     string  `json:"lifetime"`
	Type         string  `json:"type"`
	Server       *bool   `json:"server,omitempty"`
	Client       *bool   `json:"client,omitempty"`
	DefaultValue any     `json:"defaultValue"`
}

type updateConfigAvailability struct {
	Server bool `json:"server"`
	Client bool `json:"client"`
}

type updateConfigRequest struct {
	Key          string                     `json:"key,omitempty"`
	Description  *string                    `json:"description,omitempty"`
	Role         string                     `json:"role,omitempty"`
	Lifetime     string                     `json:"lifetime,omitempty"`
	Type         string                     `json:"type,omitempty"`
	Availability *updateConfigAvailability  `json:"availability,omitempty"`
}

// UpdateConfigRequest is the caller-facing shape; the API's PATCH endpoint
// nests server/client under an "availability" object, which this method
// translates to.
type UpdateConfigRequest struct {
	Key         string
	Description *string
	Role        string
	Lifetime    string
	Type        string
	Server      bool
	Client      bool
}

func (c *Client) CreateConfig(ctx context.Context, projectID string, req CreateConfigRequest) (*Config, error) {
	var out Config
	path := fmt.Sprintf("/projects/%s/configs", pathEscape(projectID))
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetConfig(ctx context.Context, projectID, key string) (*Config, error) {
	var out Config
	path := fmt.Sprintf("/projects/%s/configs/%s", pathEscape(projectID), pathEscape(key))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateConfig(ctx context.Context, projectID, key string, req UpdateConfigRequest) (*Config, error) {
	var out Config
	path := fmt.Sprintf("/projects/%s/configs/%s", pathEscape(projectID), pathEscape(key))
	body := updateConfigRequest{
		Key:         req.Key,
		Description: req.Description,
		Role:        req.Role,
		Lifetime:    req.Lifetime,
		Type:        req.Type,
		Availability: &updateConfigAvailability{
			Server: req.Server,
			Client: req.Client,
		},
	}
	if err := c.do(ctx, http.MethodPatch, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteConfig(ctx context.Context, projectID, key string) error {
	path := fmt.Sprintf("/projects/%s/configs/%s", pathEscape(projectID), pathEscape(key))
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListConfigs(ctx context.Context, projectID string) ([]Config, error) {
	var out []Config
	path := fmt.Sprintf("/projects/%s/configs", pathEscape(projectID))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
