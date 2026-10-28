package main

import (
	"context"
	"encoding/json"
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

func TestCalculateAndShareHandlers(t *testing.T) {
	service := &app{
		staticDir: ".",
		site: siteConfig{
			Name: "ByteVibe VPS Calculator", URL: "https://calc.example.test",
			Description: "Example description", Keywords: "vps,calculator",
		},
	}
	requestBody := `{"exchange_rate":"7","custom_exchange_rate":"7.5","renew_money":"120","currency_code":"USD","cycle":"annually","expiry_date":"2025-03-31","trade_date":"2025-01-01"}`
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
	if body.Status != "success" || body.Data.RemainValue == "" {
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
	if !strings.Contains(share.Body.String(), `viewBox="0 0 570 400"`) || !strings.Contains(share.Body.String(), "ByteVibe VPS Calculator") || !strings.Contains(share.Body.String(), "(总 840.000 元)") {
		t.Fatalf("SVG is missing official layout or result fields: %s", share.Body.String())
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
		`href="/brand/icon.svg?rev=2&amp;theme=light"`,
		"<h1>ByteVibe &lt;Calc&gt;</h1>",
		`title="Frankfurter 汇率来源" aria-label="Frankfurter 汇率来源"><span class="rate-source-mark rate-source-mark-fr" aria-hidden="true">F</span></a>`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("index missing %q", expected)
		}
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
