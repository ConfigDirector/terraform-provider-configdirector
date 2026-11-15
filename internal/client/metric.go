package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type metricOpenRun struct {
	ID            string `json:"id"`
	EnvironmentID string `json:"environmentId"`
	StartedAt     string `json:"startedAt"`
}

type metricUsage struct {
	ConfigKey   string          `json:"configKey"`
	Role        string          `json:"role"`
	PrimaryKind *string         `json:"primaryKind"`
	OpenRuns    []metricOpenRun `json:"openRuns"`
}

type MetricFilterCondition struct {
	Field        string   `json:"field"`
	TargetType   string   `json:"targetType"`
	Operator     string   `json:"operator"`
	TargetValues []string `json:"targetValues"`
}

type MetricRatioSide struct {
	EventName  string                  `json:"eventName"`
	Adds       string                  `json:"adds"`
	Filter     []MetricFilterCondition `json:"filter"`
	ValueField *string                 `json:"valueField"`
}

type MetricRatio struct {
	Numerator   MetricRatioSide `json:"numerator"`
	Denominator MetricRatioSide `json:"denominator"`
}

type MetricFunnelStep struct {
	EventName string                  `json:"eventName"`
	Filter    []MetricFilterCondition `json:"filter"`
}

type MetricFunnel struct {
	Steps []MetricFunnelStep `json:"steps"`
}

type MetricRetention struct {
	EventName string                  `json:"eventName"`
	Filter    []MetricFilterCondition `json:"filter"`
	FirstDay  int64                   `json:"firstDay"`
	LastDay   int64                   `json:"lastDay"`
}

type Metric struct {
	ID          string                  `json:"id"`
	ProjectID   string                  `json:"projectId"`
	EventName   *string                 `json:"eventName"`
	Filter      []MetricFilterCondition `json:"filter"`
	ValueField  *string                 `json:"valueField"`
	Ratio       *MetricRatio            `json:"ratio"`
	Funnel      *MetricFunnel           `json:"funnel"`
	Retention   *MetricRetention        `json:"retention"`
	Description *string                 `json:"description"`
	Direction   string                  `json:"direction"`
	Kinds       []string                `json:"kinds"`
	Usages      []metricUsage           `json:"usages"`
	CreatedAt   string                  `json:"createdAt"`
	UpdatedAt   string                  `json:"updatedAt"`
}

type CreateMetricRequest struct {
	EventName   *string                 `json:"eventName"`
	Filter      []MetricFilterCondition `json:"filter"`
	ValueField  *string                 `json:"valueField"`
	Ratio       *MetricRatio            `json:"ratio"`
	Funnel      *MetricFunnel           `json:"funnel"`
	Retention   *MetricRetention        `json:"retention"`
	Description *string                 `json:"description"`
	Direction   string                  `json:"direction"`
	Kinds       []string                `json:"kinds"`
}

type UpdateMetricRequest struct {
	Description *string  `json:"description"`
	Direction   string   `json:"direction"`
	Kinds       []string `json:"kinds"`
}

func (c *Client) CreateMetric(ctx context.Context, projectID string, req CreateMetricRequest) (*Metric, error) {
	var out Metric
	path := fmt.Sprintf("/v1/projects/%s/metrics", pathEscape(projectID))
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetMetric(ctx context.Context, projectID, metricID string) (*Metric, error) {
	var out Metric
	path := fmt.Sprintf("/v1/projects/%s/metrics/%s", pathEscape(projectID), pathEscape(metricID))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListMetricsWithEventName(ctx context.Context, projectID, eventName string) ([]Metric, error) {
	var out []Metric
	path := fmt.Sprintf("/v1/projects/%s/metrics?eventName=%s", pathEscape(projectID), url.QueryEscape(eventName))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) UpdateMetric(ctx context.Context, projectID, metricID string, req UpdateMetricRequest) (*Metric, error) {
	var out Metric
	path := fmt.Sprintf("/v1/projects/%s/metrics/%s", pathEscape(projectID), pathEscape(metricID))
	if err := c.do(ctx, http.MethodPatch, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteMetric(ctx context.Context, projectID, metricID string) error {
	path := fmt.Sprintf("/v1/projects/%s/metrics/%s", pathEscape(projectID), pathEscape(metricID))
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
