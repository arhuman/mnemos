package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// loadBaseline reads a baseline Metrics from path. A missing file is not an
// error: it returns (nil, nil) so callers print without deltas. Any other read
// or decode failure is wrapped and returned.
func loadBaseline(path string) (*Metrics, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a CLI-provided eval baseline artifact
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // a missing baseline file is intentionally not an error; callers print without deltas
	}
	if err != nil {
		return nil, fmt.Errorf("eval: read baseline %q: %w", path, err)
	}
	var m Metrics
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("eval: decode baseline %q: %w", path, err)
	}

	return &m, nil
}

// saveBaseline writes m to path as pretty JSON, overwriting any existing file.
func saveBaseline(path string, m Metrics) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: encode baseline: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("eval: write baseline %q: %w", path, err)
	}

	return nil
}

// writeReport prints the metrics table to out. When baseline is non-nil, each
// rate is annotated with its signed delta against the baseline, e.g.
// "Hit@1       0.71 (+0.04 vs baseline)". There is no CI gate (per plan); this
// is informational only.
func writeReport(out io.Writer, m Metrics, baseline *Metrics) {
	_, _ = fmt.Fprintf(out, "queries     %d\n", m.N)
	_, _ = fmt.Fprintf(out, "Hit@1       %s\n", metricLine(m.HitAt1, baseline, func(b Metrics) float64 { return b.HitAt1 }))
	_, _ = fmt.Fprintf(out, "Recall@%d   %s\n", m.K, metricLine(m.RecallAtK, baseline, func(b Metrics) float64 { return b.RecallAtK }))
	_, _ = fmt.Fprintf(out, "MRR@%d      %s\n", m.K, metricLine(m.MRRAtK, baseline, func(b Metrics) float64 { return b.MRRAtK }))
	_, _ = fmt.Fprintf(out, "exact-chunk %s\n", metricLine(m.ExactChunk, baseline, func(b Metrics) float64 { return b.ExactChunk }))
}

// metricLine renders a metric value to two decimals, appending a signed delta
// against the baseline when one is supplied. get selects the matching field
// from the baseline so the same formatter serves every metric.
func metricLine(value float64, baseline *Metrics, get func(Metrics) float64) string {
	if baseline == nil {
		return fmt.Sprintf("%.2f", value)
	}
	delta := value - get(*baseline)

	return fmt.Sprintf("%.2f (%+.2f vs baseline)", value, delta)
}
