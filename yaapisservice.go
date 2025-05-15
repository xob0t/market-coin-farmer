package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/mengzhuo/cookiestxt"
)

// YaApiService handles interactions with the rewards API
type YaApiService struct {
	clientCache     map[string]tls_client.HttpClient
	clientCacheLock sync.RWMutex
}

// client cache
func (rs *YaApiService) init() {
	rs.clientCacheLock.Lock()
	defer rs.clientCacheLock.Unlock()

	if rs.clientCache == nil {
		rs.clientCache = make(map[string]tls_client.HttpClient)
	}
}

// Common HTTP headers used in all requests
var commonHeaders = map[string]string{
	"accept":             "*/*",
	"accept-language":    "en-US,en;q=0.5",
	"cache-control":      "no-cache",
	"content-type":       "application/json",
	"origin":             "https://market.yandex.ru",
	"pragma":             "no-cache",
	"priority":           "u=1, i",
	"referer":            "https://market.yandex.ru/kolesoprizov?track=menu",
	"sec-ch-ua-mobile":   "?0",
	"sec-ch-ua-platform": `"Windows"`,
	"sec-fetch-dest":     "empty",
	"sec-fetch-mode":     "cors",
	"sec-fetch-site":     "same-origin",
	"sec-gpc":            "1",
	// "user-agent":            "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:138.0) Gecko/20100101 Firefox/138.0",
	"x-market-page-id": "market:fortune-wheel",
	"x-requested-with": "XMLHttpRequest",
}

// Regex patterns for extracting values
var (
	skRegex          = regexp.MustCompile(`"sk":\s*"(.*?)"`)
	loginRegex       = regexp.MustCompile(`"login":\s*"(.*?)"`)
	coinBalanceRegex = regexp.MustCompile(`"coinsAmount":(\d*),`)
)

// GetRewardsJson retrieves rewards information for an account
func (rs *YaApiService) GetRewardsJson(account *Account) (string, string, string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", "", account.Login, fmt.Errorf("failed to authenticate: %w", err)
	}

	client, err := rs.getClient(account)
	if err != nil {
		return "", "", account.Login, fmt.Errorf("failed to create client: %w", err)
	}

	data := strings.NewReader(`{"params":[{"wheel_ids":["default_wheel"]}],"path":"/kolesoprizov?track=menu"}`)
	req, err := rs.createRequest(http.MethodPost,
		"https://market.yandex.ru/api/resolve/?r=src/resolvers/cashbackLevels/fortune/resolveFortuneRewardsTab:resolveFortuneRewardsTab",
		data)
	if err != nil {
		return "", "", account.Login, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-retpath-y", "https://market.yandex.ru/kolesoprizov?track=menu")
	req.Header.Set("sk", account.TokenSK)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", account.Login, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", account.Login, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", account.Login, fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), account.Login, account.CoinBalance, nil
}

// ensureAuth checks if we have valid auth tokens and refreshes them if needed
func (rs *YaApiService) ensureAuth(account *Account) error {
	// If we have recent auth data, use it
	if account.TokenSK != "" && account.Login != "" && time.Since(account.LastAuth) < time.Hour {
		return nil
	}

	// Otherwise, fetch fresh auth data
	err := rs.getProfileInfo(account)
	if err != nil {
		return fmt.Errorf("failed to get profile info: %w", err)
	}

	account.LastAuth = time.Now()

	return nil
}

// getProfileInfo extracts SK, login and CoinBalance information from account profile
func (rs *YaApiService) getProfileInfo(account *Account) error {
	client, err := rs.getClient(account)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	req, err := rs.createRequest(http.MethodGet, "https://market.yandex.ru/kolesoprizov?track=menu", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	account.TokenSK = extractStringValue(body, skRegex)
	account.Login = extractStringValue(body, loginRegex)
	account.CoinBalance = extractStringValue(body, coinBalanceRegex)

	return nil
}

// Roll executes a spin of the prize wheel
func (rs *YaApiService) Roll(account *Account) (string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	client, err := rs.getClient(account)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}

	data := strings.NewReader(`{"params":[{"body":{"wheel_id":"default_wheel"}}],"path":"/kolesoprizov?track=menu"}`)
	req, err := rs.createRequest(http.MethodPost,
		"https://market.yandex.ru/api/resolve/?r=src/resolvers/cashbackLevels/fortune/resolveFortuneWheelSpin:resolveFortuneWheelSpin",
		data)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("sk", account.TokenSK)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if len(body) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return string(body), nil
}

// ClaimDailyCoins claims available coins from the service
func (rs *YaApiService) ClaimDailyCoins(account *Account) (string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	client, err := rs.getClient(account)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}

	data := strings.NewReader(`{"params":[null],"path":"/kolesoprizov?track=menu"}`)
	req, err := rs.createRequest(http.MethodPost,
		"https://market.yandex.ru/api/resolve/?r=src/resolvers/dailySignIn/resolveSignInReceiveReward:resolveSignInReceiveReward",
		data)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("sk", account.TokenSK)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if len(body) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return string(body), nil
}

// ClaimDailyGameReward claims game reward
func (rs *YaApiService) ClaimDailyGameReward(account *Account) (string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	client, err := rs.getClient(account)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	var jsonStr = fmt.Sprintf(`{"params":[{"token":"game_reward-%s-market"}],"path":"/kolesoprizov?track=menu"}`, today)
	var data = strings.NewReader(jsonStr)
	req, err := rs.createRequest(http.MethodPost, "https://market.yandex.ru/api/resolve/?r=src/resolvers/cashbackLevels/fortune/resolveFetchFortuneGameReward:resolveFortuneGameReward", data)

	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("sk", account.TokenSK)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if len(body) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return string(body), nil
}

// getClient creates or retrieves a cached HTTP client for the account
func (rs *YaApiService) getClient(account *Account) (tls_client.HttpClient, error) {
	// Ensure cache is initialized
	rs.init()

	cacheKey := account.Proxy + account.Cookies

	// Check if we have a cached client
	rs.clientCacheLock.RLock()
	cachedClient, exists := rs.clientCache[cacheKey]
	rs.clientCacheLock.RUnlock()

	if exists {
		return cachedClient, nil
	}

	// Create a new client if not in cache
	netscapeCookies, err := base64.StdEncoding.DecodeString(account.Cookies)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cookies: %w", err)
	}

	jar := tls_client.NewCookieJar()
	if err := loadCookiesFromNetscape(jar, string(netscapeCookies)); err != nil {
		return nil, fmt.Errorf("failed to load cookies: %w", err)
	}

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithProxyUrl(account.Proxy),
		tls_client.WithInsecureSkipVerify(),
		tls_client.WithClientProfile(profiles.Chrome_131),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithCookieJar(jar),
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	// Cache the new client
	rs.clientCacheLock.Lock()
	rs.clientCache[cacheKey] = client
	rs.clientCacheLock.Unlock()

	return client, nil
}

// createRequest creates a new HTTP request with common headers
func (rs *YaApiService) createRequest(method, url string, body io.Reader) (*fhttp.Request, error) {
	req, err := fhttp.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// Set common headers
	for k, v := range commonHeaders {
		req.Header.Set(k, v)
	}

	return req, nil
}

// loadCookiesFromNetscape loads cookies from Netscape format string into a cookie jar
func loadCookiesFromNetscape(jar tls_client.CookieJar, cookieData string) error {
	cookieReader := strings.NewReader(cookieData)
	cookies, err := cookiestxt.Parse(cookieReader)
	if err != nil {
		return fmt.Errorf("failed to parse cookies: %w", err)
	}

	for _, cookie := range cookies {
		u := &url.URL{
			Scheme: "https",
			Host:   cookie.Domain,
			Path:   cookie.Path,
		}

		httpCookie := &fhttp.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Path:     cookie.Path,
			Domain:   cookie.Domain,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HttpOnly,
		}

		jar.SetCookies(u, []*fhttp.Cookie{httpCookie})
	}
	return nil
}

// extractStringValue extracts a string value using a regex pattern
func extractStringValue(body []byte, re *regexp.Regexp) string {
	matches := re.FindSubmatch(body)
	if len(matches) > 1 {
		return string(matches[1])
	}
	return ""
}
