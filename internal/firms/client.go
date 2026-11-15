package firms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"firms-harmonizer/internal/query"
)

type AuthError struct{ Message string }

func (e *AuthError) Error() string { return e.Message }

type RequestError struct{ Message string }

func (e *RequestError) Error() string { return e.Message }

type Chunk struct {
	Source    string
	StartDate time.Time
	EndDate   time.Time
	Text      string
	FetchedAt time.Time
	URL       string
}

type Client struct {
	MapKey       string
	BaseURL      string
	HTTP         *http.Client
	MaxRetries   int
	MaxDays      int
	RequestDelay time.Duration
}

func NewClient(mapKey, baseURL string, timeout time.Duration, maxRetries, maxDays int, delay time.Duration) *Client {
	if maxDays < 1 || maxDays > 5 {
		maxDays = 5
	}
	return &Client{
		MapKey:       mapKey,
		BaseURL:      strings.TrimRight(baseURL, "/"),
		HTTP:         &http.Client{Timeout: timeout},
		MaxRetries:   maxRetries,
		MaxDays:      maxDays,
		RequestDelay: delay,
	}
}

func (c *Client) BuildURL(product Product, bbox string, start time.Time, days int) (string, error) {
	normalized, err := normalizeBBox(bbox)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/api/area/csv/%s/%s/%s/%d/%s",
		c.BaseURL, url.PathEscape(c.MapKey), product.Source, normalized, days,
		start.UTC().Format("2006-01-02")), nil
}

func (c *Client) FetchCSV(
	ctx context.Context,
	product Product,
	bbox string,
	start, end time.Time,
	consume func(Chunk) error,
) error {
	if c.MapKey == "" {
		return &AuthError{Message: "FIRMS_MAP_KEY is not set; request a free key at https://firms.modaps.eosdis.nasa.gov/api/map_key/"}
	}
	if c.MaxDays < 1 || c.MaxDays > 5 {
		return fmt.Errorf("max days per request must be between 1 and 5")
	}
	start = dateOnly(start)
	end = dateOnly(end)
	if start.After(end) {
		return fmt.Errorf("start date must be on or before end date")
	}

	for cursor := start; !cursor.After(end); {
		chunkEnd := cursor.AddDate(0, 0, c.MaxDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		days := int(chunkEnd.Sub(cursor).Hours()/24) + 1
		endpoint, err := c.BuildURL(product, bbox, cursor, days)
		if err != nil {
			return err
		}
		text, err := c.fetchWithRetries(ctx, endpoint)
		if err != nil {
			return err
		}
		if err := consume(Chunk{
			Source:    product.Source,
			StartDate: cursor,
			EndDate:   chunkEnd,
			Text:      text,
			FetchedAt: time.Now().UTC(),
			URL:       endpoint,
		}); err != nil {
			return err
		}
		cursor = chunkEnd.AddDate(0, 0, 1)
		if !cursor.After(end) && c.RequestDelay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.RequestDelay):
			}
		}
	}
	return nil
}

func (c *Client) fetchWithRetries(ctx context.Context, endpoint string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}
		response, err := c.HTTP.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<20))
			response.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else {
				switch {
				case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
					return "", &AuthError{Message: fmt.Sprintf("FIRMS rejected the MAP_KEY (HTTP %d)", response.StatusCode)}
				case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
					lastErr = &RequestError{Message: fmt.Sprintf("FIRMS transient HTTP %d", response.StatusCode)}
					if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
						if seconds, err := strconv.Atoi(retryAfter); err == nil {
							sleep(ctx, time.Duration(min(seconds, 30))*time.Second)
						}
					}
				case response.StatusCode >= 400:
					preview := strings.ReplaceAll(string(body), "\n", " ")
					if len(preview) > 300 {
						preview = preview[:300]
					}
					return "", &RequestError{Message: fmt.Sprintf("FIRMS HTTP %d: %s", response.StatusCode, preview)}
				default:
					return string(body), nil
				}
			}
		} else {
			lastErr = err
		}
		if attempt < c.MaxRetries {
			sleep(ctx, time.Duration(1<<min(attempt, 3))*time.Second)
		}
	}
	return "", &RequestError{Message: fmt.Sprintf("FIRMS request failed after retries: %v", lastErr)}
}

func normalizeBBox(value string) (string, error) {
	bbox, err := query.ParseBBox(value)
	if err != nil {
		return "", err
	}
	if bbox == nil {
		return "world", nil
	}
	return bbox.String(), nil
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func sleep(ctx context.Context, duration time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(duration):
	}
}
