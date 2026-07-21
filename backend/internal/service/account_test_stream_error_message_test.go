//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractTestStreamErrorMessage(t *testing.T) {
	t.Run("openai object error", func(t *testing.T) {
		msg := extractTestStreamErrorMessage(map[string]any{
			"type":  "error",
			"error": map[string]any{"message": "bad request"},
		}, "Unknown error")
		require.Equal(t, "bad request", msg)
	})

	t.Run("grok string error with code", func(t *testing.T) {
		msg := extractTestStreamErrorMessage(map[string]any{
			"type":  "error",
			"code":  "resource-exhausted",
			"error": "The model is currently at capacity due to high demand.",
		}, "Unknown error")
		require.Equal(t, "resource-exhausted: The model is currently at capacity due to high demand.", msg)
	})

	t.Run("string error without code", func(t *testing.T) {
		msg := extractTestStreamErrorMessage(map[string]any{
			"error": "plain failure",
		}, "Unknown error")
		require.Equal(t, "plain failure", msg)
	})

	t.Run("fallback", func(t *testing.T) {
		msg := extractTestStreamErrorMessage(map[string]any{"type": "error"}, "Unknown error")
		require.Equal(t, "Unknown error", msg)
	})
}
