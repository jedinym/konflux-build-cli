package common

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "sanitizes URL with credentials",
			url:      "https://user:pass@example.com",
			expected: "https://%2A%2A%2A@example.com",
		},
		{
			name:     "sanitizes URL without credentials",
			url:      "https://example.com",
			expected: "https://example.com",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			actual := SanitizeURL(test.url)
			g.Expect(actual).To(Equal(test.expected))
		})
	}
}
