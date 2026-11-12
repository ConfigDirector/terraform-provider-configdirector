package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var metricIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

type metricSelection interface {
	findMetric(ctx context.Context, api *client.Client, projectIDOrSlug, projectID string) (*client.Metric, diag.Diagnostics)
}

func metricSelectionOf(reference string) metricSelection {
	if metricIDPattern.MatchString(reference) {
		return metricSelectedByID{metricID: reference}
	}
	return metricSelectedByEventName{eventName: reference}
}

type metricSelectedByID struct {
	metricID string
}

func (selection metricSelectedByID) findMetric(ctx context.Context, api *client.Client, projectIDOrSlug, projectID string) (*client.Metric, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	metric, err := api.GetMetric(ctx, projectID, selection.metricID)
	var apiError *client.APIError
	switch {
	case errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound:
		diagnostics.AddError("Metric Not Found", fmt.Sprintf("Project %q has no metric with the id %q.", projectIDOrSlug, selection.metricID))
	case err != nil:
		diagnostics.AddError("Error Importing Metric", err.Error())
	}
	return metric, diagnostics
}

type metricSelectedByEventName struct {
	eventName string
}

func (selection metricSelectedByEventName) findMetric(ctx context.Context, api *client.Client, projectIDOrSlug, projectID string) (*client.Metric, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	metrics, err := api.ListMetricsWithEventName(ctx, projectID, selection.eventName)
	if err != nil {
		diagnostics.AddError("Error Importing Metric", err.Error())
		return nil, diagnostics
	}

	switch len(metrics) {
	case 0:
		diagnostics.AddError(
			"Metric Not Found",
			fmt.Sprintf("Project %q has no metric for the event name %q. The event name is matched exactly, letter case included.", projectIDOrSlug, selection.eventName),
		)
		return nil, diagnostics
	case 1:
		return &metrics[0], diagnostics
	default:
		diagnostics.AddError("Several Metrics Found", severalMetricsWithEventName(projectIDOrSlug, selection.eventName, metrics))
		return nil, diagnostics
	}
}

func severalMetricsWithEventName(projectIDOrSlug, eventName string, metrics []client.Metric) string {
	lines := []string{
		fmt.Sprintf("%d metrics in project %q have the event name %q.", len(metrics), projectIDOrSlug, eventName),
		fmt.Sprintf("Import one by its id, as %s/<metric id>:", projectIDOrSlug),
	}
	for _, metric := range metrics {
		lines = append(lines, "  "+metric.ID+"  "+metricLabel(metric))
	}
	return strings.Join(lines, "\n")
}

func metricConditionWords(condition client.MetricFilterCondition) string {
	targetType, isTargetType := metricFilterTargetTypes[condition.TargetType]
	if !isTargetType {
		return condition.Field + " " + comparisonWithTargetValueList(condition)
	}
	return condition.Field + " " + targetType.comparisonWords(condition)
}

func metricLabel(metric client.Metric) string {
	words := []string{metric.EventName}
	if len(metric.Filter) > 0 {
		conditions := make([]string, len(metric.Filter))
		for i, condition := range metric.Filter {
			conditions[i] = metricConditionWords(condition)
		}
		words = append(words, strings.Join(conditions, " and "))
	}
	if metric.ValueField != nil {
		words = append(words, "value from "+*metric.ValueField)
	}
	return strings.Join(words, " · ")
}
