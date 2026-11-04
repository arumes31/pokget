package service

import "context"

// ImageVariantsForDiagnostics exposes the full-card and magnified-crop payloads
// used by the Claude detector, so an operator can reproduce a live scan without
// deploying instrumentation.
func ImageVariantsForDiagnostics(ctx context.Context, image []byte) ([][]byte, error) {
	return claudeImageVariants(ctx, image)
}
