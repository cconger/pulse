package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var (
	errForbidden     = errors.New("forbidden")
	errCannotRefresh = errors.New("cannot refresh")
)

type TwitchClient interface {
	OAuthGetToken(context.Context, string, string) (*GetTokenResponse, error)
	GetUsersByID(context.Context, ...string) ([]*TwitchUser, error)
	UserClient(*UserAuth) UserClient
}

type UserClient interface {
	GetUser(context.Context) (*TwitchUser, error)
}

type Client struct {
	Client       *http.Client
	ClientID     string
	ClientSecret string

	auth AuthProvider
}

type AuthProvider interface {
	Token() (string, error)
	Refresh()
}

type AppAuth struct {
	ID     string
	Secret string

	once sync.Once
	t    string
}

func (a *AppAuth) Token() (string, error) {
	var err error
	a.once.Do(func() {
		a.t, err = a.getToken()
		if err != nil {
			slog.Error("getting appToken", "err", err)
			a.once = sync.Once{}
		}
	})
	return a.t, err
}

func (a *AppAuth) getToken() (string, error) {
	slog.Info("getting appToken")
	params := url.Values{}
	params.Add("client_id", a.ID)
	params.Add("client_secret", a.Secret)
	params.Add("grant_type", "client_credentials")

	req, err := http.NewRequest(http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating oauth request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post oauth request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp GetTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&tokenResp)
	if err != nil {
		return "", fmt.Errorf("deserializing oauth: %w", err)
	}

	return tokenResp.AccessToken, nil
}

func (a *AppAuth) Refresh() {
	a.once = sync.Once{}
}

type UserAuth struct {
	AccessToken  string
	RefreshToken string
}

func (ua *UserAuth) Token() (string, error) {
	return ua.AccessToken, nil
}

func (ua *UserAuth) Refresh() {
	slog.Warn("refreshing user token not implemented")
}

func NewClient(clientID string, clientSecret string, httpClient *http.Client) (*Client, error) {
	if clientID == "" {
		return nil, fmt.Errorf("client id cannot be empty")
	}

	if clientSecret == "" {
		return nil, fmt.Errorf("client secret cannot be empty")
	}

	return &Client{
		Client:       httpClient,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		auth: &AppAuth{
			ID:     clientID,
			Secret: clientSecret,
		},
	}, nil
}

func (c *Client) UserClient(ua *UserAuth) UserClient {
	return &Client{
		Client:       c.Client,
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		auth:         ua,
	}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	token, err := c.auth.Token()
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Add("Client-Id", c.ClientID)

	resp, err := c.Client.Do(req)
	if err != nil {
		return resp, err
	}

	if resp.StatusCode == http.StatusForbidden {
		return resp, errForbidden
	}

	return resp, err
}

func (c *Client) authHeaders(r *http.Request) *http.Request {
	return r
}

type GetTokenResponse struct {
	AccessToken  string   `json:"access_token"`
	ExpiresIn    int64    `json:"expires_in"`
	RefreshToken string   `json:"refresh_token"`
	Scope        []string `json:"scope"`
	TokenType    string   `json:"token_type"`
}

func (c *Client) OAuthGetToken(ctx context.Context, code string, redirectURI string) (*GetTokenResponse, error) {
	payload := url.Values{
		"client_id":     []string{c.ClientID},
		"client_secret": []string{c.ClientSecret},
		"code":          []string{code},
		"grant_type":    []string{"authorization_code"},
		"redirect_uri":  []string{redirectURI},
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response GetTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func (c *Client) OAuthRefreshToken(ctx context.Context, ua *UserAuth) (*GetTokenResponse, error) {
	payload := url.Values{
		"client_id":     []string{c.ClientID},
		"client_secret": []string{c.ClientSecret},
		"grant_type":    []string{"refresh_token"},
		"refresh_token": []string{ua.RefreshToken},
	}

	r, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://id.twitch.tv/oauth2/token",
		strings.NewReader(payload.Encode()),
	)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errCannotRefresh
	}

	var response GetTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

type TwitchUser struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
}

type TwitchUserPayload struct {
	Data []*TwitchUser `json:"data"`
}

func (c *Client) GetUser(ctx context.Context) (*TwitchUser, error) {
	users, err := c.GetUsersByLogin(ctx)
	if err != nil {
		return nil, err
	}
	if len(users) < 1 {
		return nil, fmt.Errorf("twitch user not found")
	}
	return users[0], err
}

func (c *Client) GetUsersByLogin(ctx context.Context, login ...string) ([]*TwitchUser, error) {
	u, err := url.Parse("https://api.twitch.tv/helix/users")
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"login": login,
	}
	u.RawQuery = params.Encode()

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload TwitchUserPayload
	err = json.NewDecoder(resp.Body).Decode(&payload)
	if err != nil {
		return nil, err
	}

	if len(payload.Data) < 1 {
		return nil, fmt.Errorf("no results")
	}

	return payload.Data, nil
}

// GetUsersByID retrieves the twitch users for the given twitch userids
func (c *Client) GetUsersByID(ctx context.Context, id ...string) ([]*TwitchUser, error) {
	u, err := url.Parse("https://api.twitch.tv/helix/users")
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"id": id,
	}
	u.RawQuery = params.Encode()

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload TwitchUserPayload
	err = json.NewDecoder(resp.Body).Decode(&payload)
	if err != nil {
		return nil, err
	}

	if len(payload.Data) < 1 {
		return nil, fmt.Errorf("no results")
	}

	return payload.Data, nil
}
