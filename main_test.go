package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCalculateUsesCalendarCycleLength(t *testing.T) {
	request := calculateRequest{
		ExchangeRate: "7", CustomExchangeRate: "7.5", RenewMoney: "120",
		CurrencyCode: "USD", Cycle: "annually", ExpiryDate: "2025-03-31", TradeDate: "2025-01-01",
	}
	result, err := calculate(request, httptest.NewRequest("POST", "/api/vps/jsq", nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.RemainDays != 89 {
		t.Fatalf("remain days = %d, want 89", result.RemainDays)
	}
	if result.TotalValue != "840.000" {
		t.Fatalf("total value = %q, want 840.000", result.TotalValue)
	}
	if result.Renewal != "120.00 美元 / 年" {
		t.Fatalf("renewal = %q", result.Renewal)
	}
}

func TestAddMonthsClamped(t *testing.T) {
	date, _ := time.Parse("2006-01-02", "2024-03-31")
	got := addMonthsClamped(date, -1).Format("2006-01-02")
	if got != "2024-02-29" {
		t.Fatalf("clamped date = %s, want 2024-02-29", got)
	}
}

func TestCalculateRejectsInvalidInput(t *testing.T) {
	request := calculateRequest{
		ExchangeRate: "7", CustomExchangeRate: "7", RenewMoney: "10",
		CurrencyCode: "XYZ", Cycle: "annually", ExpiryDate: "2025-03-31", TradeDate: "2025-01-01",
	}
	if _, err := calculate(request, httptest.NewRequest("POST", "/", nil)); err == nil {
		t.Fatal("expected unsupported currency error")
	}
}

func TestCalculationStatsPersistDailyAndTotalCounts(t *testing.T) {
	db, err := openStatsDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &app{db: db}
	if got := service.calculationStats(context.Background()); got.Today != 0 || got.Total != 0 {
		t.Fatalf("initial stats = %+v", got)
	}
	for range 2 {
		if err := service.recordCalculation(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := service.calculationStats(context.Background()); got.Today != 2 || got.Total != 2 {
		t.Fatalf("updated stats = %+v, want today=2 total=2", got)
	}
}

func TestRateLimiterAllowsOnlyConfiguredRequestsPerWindow(t *testing.T) {
	limiter := newRateLimiter(2, time.Minute)
	if !limiter.allow("192.0.2.1") || !limiter.allow("192.0.2.1") || limiter.allow("192.0.2.1") {
		t.Fatal("rate limiter did not enforce the per-key request limit")
	}
	if !limiter.allow("192.0.2.2") {
		t.Fatal("rate limiter should keep independent client keys")
	}
}

func TestCalculateAndShareHandlers(t *testing.T) {
	service := &app{
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != "https://calc.example.test/brand/icon.svg" {
				t.Fatalf("icon request URL = %q", request.URL)
			}
			header := make(http.Header)
			header.Set("Content-Type", "image/png")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n")),
			}, nil
		})},
		staticDir: ".",
		site: siteConfig{
			Name: "ByteVibe VPS Calculator", URL: "https://calc.example.test", IconURL: "/brand/icon.svg",
			Description: "Example description", Keywords: "vps,calculator",
		},
	}
	requestBody := `{"exchange_rate":"7","custom_exchange_rate":"7.5","renew_money":"120","transaction_amount":"250","currency_code":"USD","cycle":"annually","expiry_date":"2025-03-31","trade_date":"2025-01-01"}`
	request := httptest.NewRequest("POST", "/api/v1/calculate", strings.NewReader(requestBody))
	request.Host = "example.test"
	request.Header.Set("Origin", "https://example.test")
	response := httptest.NewRecorder()
	routes(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("calculate status = %d", response.Code)
	}
	var body struct {
		Status string     `json:"status"`
		Data   resultData `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "success" || body.Data.RemainValue == "" || body.Data.Premium != "30.548" {
		t.Fatalf("unexpected calculate response: %+v", body)
	}
	if body.Data.Renewal != "120.00 美元 / 年" {
		t.Fatalf("renewal = %q", body.Data.Renewal)
	}

	shareURL, err := url.Parse(body.Data.SharePic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shareURL.Path, "/share/") {
		t.Fatalf("share URL does not use the versioned public layout: %s", shareURL.Path)
	}
	if shareURL.Host != "calc.example.test" {
		t.Fatalf("share URL host = %q, want configured host", shareURL.Host)
	}
	shareRequest := httptest.NewRequest("GET", shareURL.Path, nil)
	share := httptest.NewRecorder()
	routes(service).ServeHTTP(share, shareRequest)
	if share.Code != http.StatusOK || !strings.Contains(share.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("share response status/content-type = %d/%q", share.Code, share.Header().Get("Content-Type"))
	}
	if !strings.Contains(share.Header().Get("Cache-Control"), "no-cache") {
		t.Fatalf("share response should revalidate cached SVGs after visual updates, Cache-Control=%q", share.Header().Get("Cache-Control"))
	}
	if !strings.Contains(share.Body.String(), `viewBox="0 0 720 500"`) || !strings.Contains(share.Body.String(), `<rect width="720" height="500" fill=`) || !strings.Contains(share.Body.String(), "ByteVibe VPS Calculator") || !strings.Contains(share.Body.String(), "840.000 元") || !strings.Contains(share.Body.String(), "年付 · 美元") || !strings.Contains(share.Body.String(), "自定义价 / 7.500") || !strings.Contains(share.Body.String(), body.Data.CustomRemainValue+" 元") || !strings.Contains(share.Body.String(), `href="data:image/png;base64,iVBORw0KGgo="`) || !strings.Contains(share.Body.String(), `x="58" y="50" width="36" height="36" preserveAspectRatio="xMidYMid meet"`) || strings.Contains(share.Body.String(), `transform="translate(54 46) scale(0.458333)"`) || !strings.Contains(share.Body.String(), "由 https://calc.example.test 提供") || !strings.Contains(share.Body.String(), "+30.548 元") || strings.Contains(share.Body.String(), "grad1") {
		t.Fatalf("SVG is missing custom ledger layout or result fields: %s", share.Body.String())
	}
	var svg struct {
		XMLName xml.Name `xml:"svg"`
	}
	if err := xml.Unmarshal(share.Body.Bytes(), &svg); err != nil || svg.XMLName.Local != "svg" {
		t.Fatalf("share response is not valid SVG XML: %v", err)
	}
}

func TestCalculateReturnsDiscountForZeroTransaction(t *testing.T) {
	request := calculateRequest{
		ExchangeRate: "7", CustomExchangeRate: "7", RenewMoney: "120", TransactionAmount: "0",
		CurrencyCode: "USD", Cycle: "annually", ExpiryDate: "2025-03-31", TradeDate: "2025-01-01",
	}
	result, err := calculate(request, httptest.NewRequest(http.MethodPost, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.TransactionAmount != "0.000" || result.Premium != "-204.822" {
		t.Fatalf("zero transaction result = amount %q, premium %q", result.TransactionAmount, result.Premium)
	}
}

func TestShareUsesBuiltInIconWhenNoSiteIconIsConfigured(t *testing.T) {
	service := &app{site: siteConfig{Name: "Test VPS", URL: "https://calc.example.test"}}
	values := url.Values{
		"trade_date": {"2025-01-01"}, "expiry_date": {"2025-03-31"},
		"currency_code": {"USD"}, "cycle": {"annually"}, "renewal": {"120.00 美元 / 年"},
		"remain_days": {"89"}, "remain_value": {"204.658"}, "exchange_rate": {"7.000"},
		"total_value": {"840.000"},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/vps/share.svg?"+values.Encode(), nil)
	response := httptest.NewRecorder()
	routes(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("share status = %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `href="data:image/svg+xml;base64,`) || strings.Contains(response.Body.String(), `transform="translate(54 46) scale(0.458333)"`) {
		t.Fatalf("SVG should embed the built-in icon when SITE_ICON_URL is unset: %s", response.Body.String())
	}
	iconRequest := httptest.NewRequest(http.MethodGet, "/static/brand-icon.svg", nil)
	iconResponse := httptest.NewRecorder()
	routes(service).ServeHTTP(iconResponse, iconRequest)
	if iconResponse.Code != http.StatusOK || !strings.Contains(iconResponse.Body.String(), `<svg`) {
		t.Fatalf("built-in site icon is not served: status=%d body=%s", iconResponse.Code, iconResponse.Body.String())
	}
}

func TestIndexUsesConfiguredSiteMetadata(t *testing.T) {
	service := &app{
		staticDir: ".",
		site: siteConfig{
			Name: "ByteVibe <Calc>", Description: `Rates & value "tool"`,
			Keywords: "vps,calculator", URL: "https://calc.example.test",
			IconURL: "/brand/icon.svg?rev=2&theme=light",
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	routes(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("index status = %d", response.Code)
	}
	page := response.Body.String()
	for _, expected := range []string{
		"<title>ByteVibe &lt;Calc&gt;</title>",
		`content="Rates &amp; value &#34;tool&#34;"`,
		`content="vps,calculator"`,
		`href="https://calc.example.test"`,
		`<link rel="icon" href="/site-icon" />`,
		`<img class="brand-mark" src="/site-icon" alt="" />`,
		`<span class="app-version">dev</span>`,
		"<h1>ByteVibe &lt;Calc&gt;</h1>",
		`title="Frankfurter 汇率来源" aria-label="Frankfurter 汇率来源"><span class="rate-source-mark rate-source-mark-fr" aria-hidden="true">F</span></a>`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("index missing %q", expected)
		}
	}
}

func TestSiteIconFallsBackToBuiltInIcon(t *testing.T) {
	service := &app{staticDir: ".", site: siteConfig{IconURL: "https://icons.test/missing.png"}, client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/site-icon", nil)
	response := httptest.NewRecorder()
	routes(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/svg+xml; charset=utf-8" || !strings.Contains(response.Body.String(), "<svg") {
		t.Fatalf("fallback icon response = %d/%q/%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestSiteConfigFromEnv(t *testing.T) {
	t.Setenv("SITE_NAME", "Test calculator")
	t.Setenv("SITE_DESCRIPTION", "A test site")
	t.Setenv("SITE_KEYWORDS", "test,calculator")
	t.Setenv("SITE_URL", "https://calc.example.test")
	t.Setenv("SITE_ICON_URL", "/icon.svg")
	config, err := siteConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Name != "Test calculator" || config.Description != "A test site" || config.Keywords != "test,calculator" || config.URL != "https://calc.example.test" || config.IconURL != "/icon.svg" {
		t.Fatalf("unexpected site config: %+v", config)
	}
	t.Setenv("SITE_ICON_URL", "not a valid icon url")
	config, err = siteConfigFromEnv()
	if err != nil || config.IconURL != "" {
		t.Fatalf("invalid icon URL should fall back to the built-in icon: config=%+v err=%v", config, err)
	}

	t.Setenv("SITE_URL", "https://calc.example.test/path")
	if _, err := siteConfigFromEnv(); err == nil {
		t.Fatal("expected SITE_URL path to be rejected")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGetRatesParsesExchangeRateAPI(t *testing.T) {
	payload := `{"result":"success","base_code":"CNY","time_last_update_unix":1793145600,"rates":{"CNY":1,"USD":0.14,"GBP":0.1,"EUR":0.12,"JPY":21,"KRW":200,"HKD":1.1,"TWD":4.5,"CAD":0.19,"SGD":0.18,"AUD":0.21}}`
	service := &app{
		provider: "exchangerate-api",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
		})},
		rateURL: "https://rates.test/v6/latest/CNY",
	}
	result, err := service.getRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Date != "2026/10/28" {
		t.Fatalf("date = %q", result.Date)
	}
	if got := result.Rates[1].Rate; got != "7.142857" {
		t.Fatalf("CNY per USD = %q, want 7.142857", got)
	}
}

func TestGetRatesParsesFrankfurter(t *testing.T) {
	payload := `[{"date":"2026-10-27","base":"CNY","quote":"USD","rate":0.14},{"date":"2026-10-27","base":"CNY","quote":"GBP","rate":0.1},{"date":"2026-10-27","base":"CNY","quote":"EUR","rate":0.12},{"date":"2026-10-27","base":"CNY","quote":"JPY","rate":21},{"date":"2026-10-27","base":"CNY","quote":"KRW","rate":200},{"date":"2026-10-27","base":"CNY","quote":"HKD","rate":1.1},{"date":"2026-10-27","base":"CNY","quote":"TWD","rate":4.5},{"date":"2026-10-27","base":"CNY","quote":"CAD","rate":0.19},{"date":"2026-10-27","base":"CNY","quote":"SGD","rate":0.18},{"date":"2026-10-27","base":"CNY","quote":"AUD","rate":0.21}]`
	service := &app{
		provider: "frankfurter",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
		})},
		rateURL: "https://rates.test/v2/rates?base=CNY",
	}
	result, err := service.getRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Date != "2026-10-27" {
		t.Fatalf("date = %q", result.Date)
	}
	if got := result.Rates[1].Rate; got != "7.142857" {
		t.Fatalf("CNY per USD = %q, want 7.142857", got)
	}
}

func TestRateProviderConfig(t *testing.T) {
	t.Setenv("RATE_PROVIDER", "ExChAnGeRaTe-Api")
	t.Setenv("EXCHANGERATE_API_KEY", "test-key")
	t.Setenv("RATES_URL", "")
	provider, endpoint, err := rateProviderConfig()
	if err != nil {
		t.Fatal(err)
	}
	if provider != "exchangerate-api" || endpoint != "https://v6.exchangerate-api.com/v6/test-key/latest/CNY" {
		t.Fatalf("unexpected configuration: provider=%q endpoint=%q", provider, endpoint)
	}

	t.Setenv("RATE_PROVIDER", "unknown")
	if _, _, err := rateProviderConfig(); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
