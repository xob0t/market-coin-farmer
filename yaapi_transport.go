package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/mengzhuo/cookiestxt"
)

func (rs *YaApiService) ensureAuth(ctx context.Context, account *Account) error {
	rs.init()
	if account.TokenSK != "" && account.Login != "" && account.AppVersion != "" && account.FrontGlue != "" && time.Since(account.LastAuth) < time.Hour {
		return nil
	}

	cacheKey := account.Proxy + account.Cookies
	rs.authCacheLock.RLock()
	cached, exists := rs.authCache[cacheKey]
	rs.authCacheLock.RUnlock()
	if exists && time.Since(cached.FetchedAt) < time.Hour {
		account.TokenSK = cached.TokenSK
		account.Login = cached.Login
		account.CoinBalance = cached.CoinBalance
		account.AppVersion = cached.AppVersion
		account.FrontGlue = cached.FrontGlue
		account.Language = cached.Language
		account.LastAuth = cached.FetchedAt
		return nil
	}

	if err := rs.getProfileInfo(ctx, account); err != nil {
		return fmt.Errorf("failed to get profile info: %w", err)
	}
	account.LastAuth = time.Now()
	rs.authCacheLock.Lock()
	rs.authCache[cacheKey] = marketAuthContext{
		TokenSK:     account.TokenSK,
		Login:       account.Login,
		CoinBalance: account.CoinBalance,
		AppVersion:  account.AppVersion,
		FrontGlue:   account.FrontGlue,
		Language:    account.Language,
		FetchedAt:   account.LastAuth,
	}
	rs.authCacheLock.Unlock()
	return nil
}

func (rs *YaApiService) getProfileInfo(ctx context.Context, account *Account) error {
	client, err := rs.getClient(account)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	req, err := rs.createNavigationRequest(ctx, fortuneURL)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return errIPBlocked
	}
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
	account.AppVersion = extractStringValue(body, appVersionRegex)
	account.FrontGlue = extractStringValue(body, frontGlueRegex)
	account.Language = extractStringValue(body, languageRegex)
	if account.Language == "" {
		account.Language = "ru"
	}

	if account.TokenSK == "" || account.Login == "" {
		return fmt.Errorf("authenticated profile data is missing")
	}
	if account.AppVersion == "" || account.FrontGlue == "" {
		return fmt.Errorf("market request context is missing")
	}
	return nil
}

func (rs *YaApiService) doJSON(ctx context.Context, account *Account, method, requestURL, target string, payload any) ([]byte, error) {
	client, err := rs.getClient(account)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}
	req, err := rs.createAPIRequest(ctx, account, method, requestURL, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response: %w", readErr)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, errIPBlocked
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return body, nil
}

func (rs *YaApiService) createNavigationRequest(ctx context.Context, requestURL string) (*fhttp.Request, error) {
	req, err := fhttp.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	headers := map[string]string{
		"accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"accept-language":           "en-US,en;q=0.9",
		"cache-control":             "no-cache",
		"pragma":                    "no-cache",
		"priority":                  "u=0, i",
		"referer":                   marketOrigin + "/",
		"sec-ch-ua":                 browserClientHint,
		"sec-ch-ua-mobile":          "?0",
		"sec-ch-ua-platform":        `"Windows"`,
		"sec-fetch-dest":            "document",
		"sec-fetch-mode":            "navigate",
		"sec-fetch-site":            "same-origin",
		"sec-fetch-user":            "?1",
		"upgrade-insecure-requests": "1",
		"user-agent":                browserUserAgent,
	}
	setHeaders(req, headers)
	return req, nil
}

func (rs *YaApiService) createAPIRequest(ctx context.Context, account *Account, method, requestURL, target string, body io.Reader) (*fhttp.Request, error) {
	req, err := fhttp.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	acceptLanguage := "en-US,en;q=0.9"
	if strings.Contains(requestURL, "/api/web/") {
		acceptLanguage = account.Language
	}
	headers := map[string]string{
		"accept":                  "*/*",
		"accept-language":         acceptLanguage,
		"cache-control":           "no-cache",
		"content-type":            "application/json",
		"origin":                  marketOrigin,
		"pragma":                  "no-cache",
		"priority":                "u=1, i",
		"referer":                 fortuneURL,
		"sec-ch-ua":               browserClientHint,
		"sec-ch-ua-mobile":        "?0",
		"sec-ch-ua-platform":      `"Windows"`,
		"sec-fetch-dest":          "empty",
		"sec-fetch-mode":          "cors",
		"sec-fetch-site":          "same-origin",
		"sk":                      account.TokenSK,
		"user-agent":              browserUserAgent,
		"x-market-app-version":    account.AppVersion,
		"x-market-apphost-target": target,
		"x-market-core-service":   "<UNKNOWN>",
		"x-market-front-glue":     account.FrontGlue,
		"x-market-page-id":        fortunePageID,
		"x-requested-with":        "XMLHttpRequest",
		"x-retpath-y":             fortuneURL,
	}
	setHeaders(req, headers)
	return req, nil
}

func setHeaders(req *fhttp.Request, headers map[string]string) {
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
}

func (rs *YaApiService) getClient(account *Account) (tls_client.HttpClient, error) {
	rs.init()
	cacheKey := account.Proxy + account.Cookies

	rs.clientCacheLock.RLock()
	cachedClient, exists := rs.clientCache[cacheKey]
	rs.clientCacheLock.RUnlock()
	if exists {
		return cachedClient, nil
	}

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
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithCookieJar(jar),
	}
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	rs.clientCacheLock.Lock()
	rs.clientCache[cacheKey] = client
	rs.clientCacheLock.Unlock()
	return client, nil
}

func loadCookiesFromNetscape(jar tls_client.CookieJar, cookieData string) error {
	cookieReader := strings.NewReader(cookieData)
	cookies, err := cookiestxt.Parse(cookieReader)
	if err != nil {
		return fmt.Errorf("failed to parse cookies: %w", err)
	}

	for _, cookie := range cookies {
		domain := strings.TrimPrefix(cookie.Domain, ".")
		u := &url.URL{
			Scheme: "https",
			Host:   domain,
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

func extractStringValue(body []byte, re *regexp.Regexp) string {
	matches := re.FindSubmatch(body)
	if len(matches) > 1 {
		return string(matches[1])
	}
	return ""
}
