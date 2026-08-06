// Package preview fetches a URL and extracts preview metadata from it.
package preview

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Result is the metadata extracted from a previewed URL.
type Result struct {
	URL            string `json:"url"`
	FinalURL       string `json:"final_url"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Status         int    `json:"status"`
	ResponseTimeMs int64  `json:"response_time_ms"`
}

// Fetcher fetches URLs and builds a preview Result for each of them.
type Fetcher struct {
	client *http.Client
}

// NewFetcher creates a Fetcher whose requests time out after the given duration.
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Fetch retrieves rawURL and extracts its preview metadata.
func (f *Fetcher) Fetch(rawURL string) (*Result, error) {
	start := time.Now()

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching url: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	elapsed := time.Since(start)

	result := &Result{
		URL:            rawURL,
		FinalURL:       resp.Request.URL.String(),
		Status:         resp.StatusCode,
		ResponseTimeMs: elapsed.Milliseconds(),
	}

	title, description := extractMetadata(body)
	result.Title = title
	result.Description = description

	return result, nil
}

// extractMetadata parses the page title and description out of HTML content.
func extractMetadata(html []byte) (title, description string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return "", ""
	}

	title = strings.TrimSpace(doc.Find("title").First().Text())

	if content, ok := doc.Find(`meta[name="description"]`).First().Attr("content"); ok {
		description = strings.TrimSpace(content)
	} else if content, ok := doc.Find(`meta[property="og:description"]`).First().Attr("content"); ok {
		description = strings.TrimSpace(content)
	}

	return title, description
}
