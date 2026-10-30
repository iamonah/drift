package loz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/iamonah/drift/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingBody struct {
	reader io.Reader
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

type fakeHTTPClient struct {
	status int
	body   *trackingBody
	err    error
	calls  int
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}

	if f.body == nil {
		f.body = &trackingBody{reader: strings.NewReader(`{"data":[]}`)}
	}

	return &http.Response{
		StatusCode: f.status,
		Status:     fmt.Sprintf("%d %s", f.status, http.StatusText(f.status)),
		Body:       f.body,
	}, nil
}

func newTestClient(status int, body string) (*Client, *fakeHTTPClient) {
	httpClient := &fakeHTTPClient{
		status: status,
		body:   &trackingBody{reader: strings.NewReader(body)},
	}

	return NewClient("http://example.test", httpClient), httpClient
}

func TestGetMonstersClosesResponseBody(t *testing.T) {
	client, httpClient := newTestClient(http.StatusOK, `{"data":[]}`)

	_, err := client.GetMonsters(context.Background())
	require.NoError(t, err)
	assert.True(t, httpClient.body.closed)
}

func TestGetMonstersSetsGameQuery(t *testing.T) {
	var gotURL string
	httpClient := &fakeHTTPClient{
		status: http.StatusOK,
		body:   &trackingBody{reader: strings.NewReader(`{"data":[]}`)},
	}

	client := NewClient("http://example.test", roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotURL = req.URL.String()
		return httpClient.Do(req)
	}))

	_, err := client.GetMonsters(context.Background())
	require.NoError(t, err)
	assert.Contains(t, gotURL, "game=totk")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestGetMonstersStatusClassification(t *testing.T) {
	tests := []struct {
		status    int
		permanent bool
	}{
		{http.StatusRequestTimeout, false},
		{http.StatusTooEarly, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusServiceUnavailable, false},
		{http.StatusGatewayTimeout, false},
		{http.StatusBadRequest, true},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, true},
		{http.StatusConflict, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusNotImplemented, true},
		{http.StatusHTTPVersionNotSupported, true},
	}

	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			client, httpClient := newTestClient(test.status, `{"data":[]}`)

			_, err := client.GetMonsters(context.Background())
			require.Error(t, err)
			assert.Equal(t, test.permanent, retry.IsPermanent(err))
			assert.True(t, httpClient.body.closed)
		})
	}
}

func TestGetMonstersNetworkErrorIsTransient(t *testing.T) {
	client, httpClient := newTestClient(http.StatusOK, `{"data":[]}`)
	httpClient.err = errors.New("connection reset by peer")

	_, err := client.GetMonsters(context.Background())
	require.Error(t, err)
	assert.False(t, retry.IsPermanent(err))
}

func TestGetMonstersMalformedResponses(t *testing.T) {
	t.Run("truncated body is transient", func(t *testing.T) {
		client, _ := newTestClient(http.StatusOK, `{"data": [`)

		_, err := client.GetMonsters(context.Background())
		require.Error(t, err)
		assert.False(t, retry.IsPermanent(err))
	})

	t.Run("wrong field type is permanent", func(t *testing.T) {
		client, _ := newTestClient(http.StatusOK, `{"data":"not-an-array"}`)

		_, err := client.GetMonsters(context.Background())
		require.Error(t, err)
		assert.True(t, retry.IsPermanent(err))
	})
}

func TestGetMonstersJSONNullReturnsNonNilResponse(t *testing.T) {
	client, _ := newTestClient(http.StatusOK, `null`)

	response, err := client.GetMonsters(context.Background())
	require.NoError(t, err)
	require.NotNil(t, response, "a JSON null must not become a nil response")
	assert.Empty(t, response.Data)
}
