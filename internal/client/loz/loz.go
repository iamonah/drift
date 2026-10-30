package loz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/iamonah/drift/internal/retry"
)

const BaseUrl = "https://api.hyrule-compendium.com/v3/compendium"

type HttpClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	baseUrl    string
	httpClient HttpClient
}

func NewClient(baseUrl string, httpClient HttpClient) *Client {
	return &Client{
		baseUrl:    baseUrl,
		httpClient: httpClient,
	}
}

type Monster struct {
	Name            string   `json:"name"`
	ID              int      `json:"id"`
	Category        string   `json:"category"`
	Description     string   `json:"description"`
	Image           string   `json:"image"`
	CommonLocations []string `json:"common_locations"`
	Drops           []string `json:"drops"`
	Dlc             bool     `json:"dlc"`
}

type GetMonstersResponse struct {
	Data []Monster `json:"data"`
}

func (c *Client) GetMonsters(ctx context.Context) (*GetMonstersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseUrl+"/category/monsters", nil)
	if err != nil {
		return nil, retry.Permanent(fmt.Errorf("failed to get monsters request: %w", err))
	}

	reqURL := req.URL
	queryParams := req.URL.Query()
	queryParams.Set("game", "totk")
	reqURL.RawQuery = queryParams.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to submit monsters http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("failed to get monsters http response: %s", resp.Status)
		if retryableStatus(resp.StatusCode) {
			return nil, statusErr
		}
		return nil, retry.Permanent(statusErr)
	}

	var response GetMonstersResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		decodeErr := fmt.Errorf("failed to unmarshal monsters http response: %w", err)
		// A truncated or empty body may be a temporary transport failure. Other
		// decoding errors describe a payload that will not become valid on retry.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, decodeErr
		}
		return nil, retry.Permanent(decodeErr)
	}

	return &response, nil
}

// retryableStatus reports whether an HTTP status can represent a temporary
// dependency failure. Authentication, authorization, and other client errors
// are permanent.
func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}

	return status >= 500 &&
		status != http.StatusNotImplemented &&
		status != http.StatusHTTPVersionNotSupported
}
