package scraper

import (
	"bytes"
	"fmt"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// ParseGauge reads a Prometheus text exposition and returns the value for a gauge metric name.
func ParseGauge(metricsText []byte, name string) (float64, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(metricsText))
	if err != nil {
		return 0, fmt.Errorf("parse prometheus: %w", err)
	}

	mf, ok := families[name]
	if !ok || mf == nil {
		return 0, fmt.Errorf("metric %q not found", name)
	}
	if mf.GetType() != dto.MetricType_GAUGE {
		return 0, fmt.Errorf("metric %q is not a gauge", name)
	}

	metrics := mf.GetMetric()
	if len(metrics) == 0 || metrics[0].GetGauge() == nil {
		return 0, fmt.Errorf("metric %q has no gauge value", name)
	}

	return metrics[0].GetGauge().GetValue(), nil
}
