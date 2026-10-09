package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type Tag struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type TagNameRequest struct {
	Name string `json:"name"`
}

func (c *Client) CreateTag(ctx context.Context, projectID string, req TagNameRequest) (*Tag, error) {
	var out Tag
	path := fmt.Sprintf("/v1/projects/%s/tags", pathEscape(projectID))
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetTag(ctx context.Context, projectID, tagID string) (*Tag, error) {
	var out Tag
	path := fmt.Sprintf("/v1/projects/%s/tags/%s", pathEscape(projectID), pathEscape(tagID))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetTagByName(ctx context.Context, projectID, name string) (*Tag, error) {
	var out Tag
	path := fmt.Sprintf("/v1/projects/%s/tags/by-name?name=%s", pathEscape(projectID), url.QueryEscape(name))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateTag(ctx context.Context, projectID, tagID string, req TagNameRequest) (*Tag, error) {
	var out Tag
	path := fmt.Sprintf("/v1/projects/%s/tags/%s", pathEscape(projectID), pathEscape(tagID))
	if err := c.do(ctx, http.MethodPut, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteTag(ctx context.Context, projectID, tagID string) error {
	path := fmt.Sprintf("/v1/projects/%s/tags/%s", pathEscape(projectID), pathEscape(tagID))
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
