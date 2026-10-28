package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var currencies = []string{"CNY", "USD", "GBP", "EUR", "JPY", "KRW", "HKD", "TWD", "CAD", "SGD", "AUD"}

type app struct {
	client    *http.Client
	db        *sql.DB
	provider  string
	rateURL   string
	staticDir string
	site      siteConfig
	mu        sync.Mutex
	cached    rateResult
	cacheAt   time.Time
	limiter   *rateLimiter
}

type rateLimitEntry struct {
	started time.Time
	count   int
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]rateLimitEntry
}

type siteConfig struct {
	Name        string
	Description string
	Keywords    string
	URL         string
	IconURL     string
}

type rateResult struct {
	Date  string         `json:"last_update_date"`
	Rates []currencyRate `json:"rates"`
}

type currencyRate struct {
	Code string `json:"currency_code"`
	Rate string `json:"rate"`
}

type calculateRequest struct {
	ExchangeRate       string `json:"exchange_rate"`
	CustomExchangeRate string `json:"custom_exchange_rate"`
	RenewMoney         string `json:"renew_money"`
	TransactionAmount  string `json:"transaction_amount"`
	CurrencyCode       string `json:"currency_code"`
	Cycle              string `json:"cycle"`
	ExpiryDate         string `json:"expiry_date"`
	TradeDate          string `json:"trade_date"`
}

type resultData struct {
	TradeDate          string `json:"trade_date"`
	ExchangeRate       string `json:"exchange_rate"`
	Renewal            string `json:"renewal"`
	RemainDays         int    `json:"remain_days"`
	ExpiryDate         string `json:"expiry_date"`
	RemainValue        string `json:"remain_value"`
	TotalValue         string `json:"total_value"`
	CustomRemainValue  string `json:"custom_remain_value"`
	CustomExchangeRate string `json:"custom_exchange_rate"`
	TransactionAmount  string `json:"transaction_amount"`
	Premium            string `json:"premium"`
	SharePic           string `json:"share_pic"`
}

func main() {
	port := env("PORT", "8080")
	provider, rateURL, err := rateProviderConfig()
	if err != nil {
		log.Fatal(err)
	}
	site, err := siteConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	db, err := openStatsDB(env("DB_PATH", "/data/vps-jsq.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	service := &app{
		db:        db,
		client:    &http.Client{Timeout: 8 * time.Second},
		provider:  provider,
		rateURL:   rateURL,
		staticDir: env("STATIC_DIR", "."),
		site:      site,
		limiter:   newRateLimiter(envInt("CALCULATE_RATE_LIMIT", 30), envDuration("CALCULATE_RATE_WINDOW", time.Minute)),
	}
	server := &http.Server{Addr: ":" + port, Handler: routes(service), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("VPS calculator listening on :%s", port)
	log.Fatal(server.ListenAndServe())
}

func routes(service *app) *http.ServeMux {
	mux := http.NewServeMux()
	// Versioned API is canonical; legacy aliases keep existing clients working.
	mux.HandleFunc("GET /api/v1/rates", service.rates)
	mux.HandleFunc("POST /api/v1/calculate", service.calculate)
	mux.HandleFunc("GET /api/vps/rates", service.rates)
	mux.HandleFunc("POST /api/vps/jsq", service.calculate)
	mux.HandleFunc("GET /api/vps/share.svg", service.share)
	mux.HandleFunc("GET /site-icon", service.siteIcon)
	mux.HandleFunc("GET /share/", service.sharedImage)
	mux.HandleFunc("GET /uploads/images/jsq/", service.sharedImage)
	files := http.FileServer(http.Dir(service.staticDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			service.index(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	return mux
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(filepath.Join(a.staticDir, "index.html"))
	if err != nil {
		http.Error(w, "页面文件不可用", http.StatusInternalServerError)
		return
	}
	canonical := a.site.URL
	if canonical == "" {
		canonical = requestBaseURL(r)
	}
	stats := a.calculationStats(r.Context())
	content = []byte(strings.NewReplacer(
		"__SITE_NAME__", html.EscapeString(a.site.Name),
		"__SITE_DESCRIPTION__", html.EscapeString(a.site.Description),
		"__SITE_KEYWORDS__", html.EscapeString(a.site.Keywords),
		"__SITE_URL__", html.EscapeString(canonical),
		"__TODAY_CALCULATIONS__", strconv.FormatInt(stats.Today, 10),
		"__TOTAL_CALCULATIONS__", strconv.FormatInt(stats.Total, 10),
		"__RATE_ATTRIBUTION__", a.rateAttributionHTML(),
	).Replace(string(content)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(content)
}

func (a *app) rateAttributionHTML() string {
	switch a.provider {
	case "exchangerate-api":
		return `<a class="rate-source-link" href="https://www.exchangerate-api.com/" target="_blank" rel="noopener noreferrer" title="Rates By Exchange Rate API" aria-label="Rates By Exchange Rate API"><span class="rate-source-mark rate-source-mark-er" aria-hidden="true">ER</span></a>`
	default:
		return `<a class="rate-source-link" href="https://frankfurter.dev/" target="_blank" rel="noopener noreferrer" title="Frankfurter 汇率来源" aria-label="Frankfurter 汇率来源"><span class="rate-source-mark rate-source-mark-fr" aria-hidden="true">F</span></a>`
	}
}

func defaultSiteConfig() siteConfig {
	return siteConfig{
		Name:        "VPS 剩余价值计算器",
		Description: "计算 VPS 剩余价值和剩余时间，支持多种货币、付款周期及 SVG 分享。",
		Keywords:    "VPS,剩余价值,计算器,续费,汇率",
	}
}

func siteConfigFromEnv() (siteConfig, error) {
	config := defaultSiteConfig()
	if value := strings.TrimSpace(os.Getenv("SITE_NAME")); value != "" {
		config.Name = value
	}
	if value := strings.TrimSpace(os.Getenv("SITE_DESCRIPTION")); value != "" {
		config.Description = value
	}
	if value := strings.TrimSpace(os.Getenv("SITE_KEYWORDS")); value != "" {
		config.Keywords = value
	}
	if value := strings.TrimSpace(os.Getenv("SITE_URL")); value != "" {
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return siteConfig{}, errors.New("SITE_URL must be an http(s) origin, such as https://example.com")
		}
		config.URL = parsed.Scheme + "://" + parsed.Host
	}
	if value := strings.TrimSpace(os.Getenv("SITE_ICON_URL")); value != "" {
		parsed, err := url.Parse(value)
		isAbsolute := err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
		isLocalPath := err == nil && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && parsed.Host == "" && parsed.Scheme == ""
		if isAbsolute || isLocalPath {
			config.IconURL = value
		}
	}
	return config, nil
}

func (a *app) rates(w http.ResponseWriter, r *http.Request) {
	result, err := a.getRates(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "汇率服务暂时不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": result})
}

func (a *app) getRates(ctx context.Context) (rateResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cached.Rates) > 0 && time.Since(a.cacheAt) < 24*time.Hour {
		return a.cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.rateURL, nil)
	if err != nil {
		return rateResult{}, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return a.cachedOrError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return a.cachedOrError(fmt.Errorf("rate provider status: %s", resp.Status))
	}
	var lookup map[string]float64
	var date string
	switch a.provider {
	case "frankfurter":
		var rows []struct {
			Date  string  `json:"date"`
			Base  string  `json:"base"`
			Quote string  `json:"quote"`
			Rate  float64 `json:"rate"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			return a.cachedOrError(err)
		}
		if len(rows) == 0 {
			return a.cachedOrError(errors.New("empty Frankfurter rates response"))
		}
		date = rows[0].Date
		lookup = map[string]float64{"CNY": 1}
		for _, row := range rows {
			if row.Base == "CNY" && row.Rate > 0 && !math.IsNaN(row.Rate) && !math.IsInf(row.Rate, 0) {
				lookup[row.Quote] = 1 / row.Rate
			}
		}
	case "exchangerate-api":
		var payload struct {
			Result          string             `json:"result"`
			BaseCode        string             `json:"base_code"`
			LastUpdateUnix  int64              `json:"time_last_update_unix"`
			LastUpdateUTC   string             `json:"time_last_update_utc"`
			ConversionRates map[string]float64 `json:"conversion_rates"`
			Rates           map[string]float64 `json:"rates"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return a.cachedOrError(err)
		}
		if payload.Result != "success" {
			return a.cachedOrError(fmt.Errorf("ExchangeRate-API returned %q", payload.Result))
		}
		if payload.BaseCode == "" {
			payload.BaseCode = "CNY"
		}
		providerRates := payload.ConversionRates
		if len(providerRates) == 0 {
			providerRates = payload.Rates
		}
		if len(providerRates) == 0 {
			return a.cachedOrError(errors.New("empty ExchangeRate-API rates response"))
		}
		date = payload.LastUpdateUTC
		if payload.LastUpdateUnix > 0 {
			date = time.Unix(payload.LastUpdateUnix, 0).UTC().Format("2006/01/02")
		} else if parsed, err := time.Parse(time.RFC1123, date); err == nil {
			date = parsed.UTC().Format("2006/01/02")
		}
		lookup = map[string]float64{"CNY": 1}
		for code, providerRate := range providerRates {
			if providerRate <= 0 || math.IsNaN(providerRate) || math.IsInf(providerRate, 0) {
				continue
			}
			if payload.BaseCode == "CNY" {
				lookup[code] = 1 / providerRate
			} else if code == "CNY" {
				lookup[payload.BaseCode] = providerRate
			}
		}
	default:
		return a.cachedOrError(fmt.Errorf("unsupported rate provider %q", a.provider))
	}
	result := rateResult{Date: date}
	for _, code := range currencies {
		rate, ok := lookup[code]
		if !ok {
			return a.cachedOrError(fmt.Errorf("missing rate for %s", code))
		}
		result.Rates = append(result.Rates, currencyRate{Code: code, Rate: strconv.FormatFloat(rate, 'f', 6, 64)})
	}
	a.cached, a.cacheAt = result, time.Now()
	return result, nil
}

func (a *app) cachedOrError(err error) (rateResult, error) {
	if len(a.cached.Rates) > 0 {
		return a.cached, nil
	}
	return rateResult{}, err
}

func (a *app) calculate(w http.ResponseWriter, r *http.Request) {
	if a.limiter != nil && !a.limiter.allow(clientIP(r)) {
		retryAfter := int(math.Ceil(a.limiter.window.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input calculateRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求数据格式无效")
		return
	}
	result, err := calculate(input, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.recordCalculation(r.Context()); err != nil {
		log.Printf("record calculation statistics: %v", err)
	}
	result.SharePic = a.makeShareURL(r, input, result)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": result})
}

type calculationStats struct {
	Today int64
	Total int64
}

func openStatsDB(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS daily_calculations (day TEXT PRIMARY KEY, count INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func (a *app) recordCalculation(ctx context.Context) error {
	if a.db == nil {
		return nil
	}
	day := time.Now().In(statsLocation()).Format("2006-01-02")
	_, err := a.db.ExecContext(ctx, `INSERT INTO daily_calculations(day, count) VALUES (?, 1) ON CONFLICT(day) DO UPDATE SET count = count + 1`, day)
	return err
}

func (a *app) calculationStats(ctx context.Context) calculationStats {
	if a.db == nil {
		return calculationStats{}
	}
	day := time.Now().In(statsLocation()).Format("2006-01-02")
	var stats calculationStats
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(count, 0) FROM daily_calculations WHERE day = ?`, day).Scan(&stats.Today); err != nil && err != sql.ErrNoRows {
		return calculationStats{}
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(count), 0) FROM daily_calculations`).Scan(&stats.Total); err != nil {
		return calculationStats{}
	}
	return stats
}

func statsLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.entries[key]; ok && now.Sub(entry.started) < l.window {
		if entry.count >= l.limit {
			return false
		}
		entry.count++
		l.entries[key] = entry
		return true
	}
	l.entries[key] = rateLimitEntry{started: now, count: 1}
	return true
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &rateLimiter{limit: limit, window: window, entries: make(map[string]rateLimitEntry)}
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"X-Real-IP", "X-Forwarded-For"} {
		if value := strings.TrimSpace(strings.Split(r.Header.Get(header), ",")[0]); value != "" {
			if ip := net.ParseIP(value); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
	}
	if r.RemoteAddr != "" {
		if ip := net.ParseIP(r.RemoteAddr); ip != nil {
			return ip.String()
		}
	}
	return "unknown"
}

func calculate(input calculateRequest, r *http.Request) (resultData, error) {
	amount, err := positiveNumber(input.RenewMoney, "续费金额")
	if err != nil {
		return resultData{}, err
	}
	rate, err := positiveNumber(input.ExchangeRate, "外币汇率")
	if err != nil {
		return resultData{}, err
	}
	customRate, err := positiveNumber(input.CustomExchangeRate, "自定义汇率")
	if err != nil {
		return resultData{}, err
	}
	if !contains(currencies, input.CurrencyCode) {
		return resultData{}, errors.New("不支持的货币")
	}
	months, ok := map[string]int{"monthly": 1, "quarterly": 3, "semiannually": 6, "annually": 12, "biennially": 24, "triennially": 36, "quinquennially": 60}[input.Cycle]
	if !ok {
		return resultData{}, errors.New("不支持的付款周期")
	}
	expiry, err := time.Parse("2006-01-02", input.ExpiryDate)
	if err != nil {
		return resultData{}, errors.New("到期日期格式无效")
	}
	trade, err := time.Parse("2006-01-02", input.TradeDate)
	if err != nil {
		return resultData{}, errors.New("交易日期格式无效")
	}
	if trade.After(expiry) {
		return resultData{}, errors.New("交易日期不能在到期时间之后")
	}
	periodStart := addMonthsClamped(expiry, -months)
	periodDays := int(expiry.Sub(periodStart).Hours() / 24)
	remainDays := int(expiry.Sub(trade).Hours() / 24)
	if periodDays <= 0 {
		return resultData{}, errors.New("付款周期无效")
	}
	total := amount * rate
	remain := total * float64(remainDays) / float64(periodDays)
	customRemain := amount * customRate * float64(remainDays) / float64(periodDays)
	transactionAmount := ""
	premium := ""
	if strings.TrimSpace(input.TransactionAmount) != "" {
		dealAmount, err := nonNegativeNumber(input.TransactionAmount, "交易金额")
		if err != nil {
			return resultData{}, err
		}
		valueBasis := remain
		if fmt.Sprintf("%.3f", customRate) != fmt.Sprintf("%.3f", rate) {
			valueBasis = customRemain
		}
		transactionAmount = fmt.Sprintf("%.3f", dealAmount)
		premium = fmt.Sprintf("%.3f", dealAmount-valueBasis)
	}
	currencyName := map[string]string{"CNY": "人民币", "USD": "美元", "GBP": "英镑", "EUR": "欧元", "JPY": "日元", "KRW": "韩元", "HKD": "港元", "TWD": "新台币", "CAD": "加拿大元", "SGD": "新加坡元", "AUD": "澳大利亚元"}[input.CurrencyCode]
	base := resultData{
		TradeDate: trade.Format("2006-01-02"), ExchangeRate: fmt.Sprintf("%.3f", rate),
		Renewal: fmt.Sprintf("%.2f %s / 年", amount*12/float64(months), currencyName), RemainDays: remainDays,
		ExpiryDate: expiry.Format("2006-01-02"), RemainValue: fmt.Sprintf("%.3f", remain),
		TotalValue: fmt.Sprintf("%.3f", total), CustomRemainValue: fmt.Sprintf("%.3f", customRemain),
		CustomExchangeRate: fmt.Sprintf("%.3f", customRate), TransactionAmount: transactionAmount, Premium: premium,
	}
	return base, nil
}

func addMonthsClamped(date time.Time, months int) time.Time {
	year, month, day := date.Date()
	month += time.Month(months)
	first := time.Date(year, month, 1, 0, 0, 0, 0, date.Location())
	lastDay := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, date.Location()).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, date.Location())
}

func (a *app) makeShareURL(r *http.Request, input calculateRequest, result resultData) string {
	baseURL := a.site.URL
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}
	values := url.Values{
		"trade_date": {result.TradeDate}, "expiry_date": {result.ExpiryDate},
		"currency_code": {input.CurrencyCode}, "cycle": {input.Cycle},
		"renewal": {result.Renewal}, "remain_days": {strconv.Itoa(result.RemainDays)},
		"remain_value": {result.RemainValue}, "exchange_rate": {result.ExchangeRate},
		"total_value": {result.TotalValue}, "custom_remain_value": {result.CustomRemainValue},
		"custom_exchange_rate": {result.CustomExchangeRate},
		"transaction_amount":   {result.TransactionAmount}, "premium": {result.Premium},
		"created_date": {time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")},
	}
	id := base64.RawURLEncoding.EncodeToString([]byte(values.Encode()))
	return strings.TrimRight(baseURL, "/") + "/share/" + id + ".svg"
}

func (a *app) share(w http.ResponseWriter, r *http.Request) {
	// Keep the old query endpoint working for links generated by earlier builds.
	a.renderShare(w, r, r.URL.Query())
}

func (a *app) sharedImage(w http.ResponseWriter, r *http.Request) {
	prefix := "/share/"
	if strings.HasPrefix(r.URL.Path, "/uploads/images/jsq/") {
		prefix = "/uploads/images/jsq/"
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), ".svg")
	if strings.Contains(id, "/") || id == r.URL.Path {
		http.NotFound(w, r)
		return
	}
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(decoded) > 2048 {
		http.NotFound(w, r)
		return
	}
	values, err := url.ParseQuery(string(decoded))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.renderShare(w, r, values)
}

func (a *app) renderShare(w http.ResponseWriter, r *http.Request, q url.Values) {
	for _, key := range []string{"trade_date", "expiry_date", "currency_code", "cycle", "renewal", "remain_days", "remain_value", "exchange_rate"} {
		if q.Get(key) == "" {
			http.Error(w, "missing share field", http.StatusBadRequest)
			return
		}
	}
	if _, err := time.Parse("2006-01-02", q.Get("trade_date")); err != nil {
		http.Error(w, "invalid trade_date", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("2006-01-02", q.Get("expiry_date")); err != nil {
		http.Error(w, "invalid expiry_date", http.StatusBadRequest)
		return
	}
	if _, err := strconv.Atoi(q.Get("remain_days")); err != nil {
		http.Error(w, "invalid remain_days", http.StatusBadRequest)
		return
	}
	if q.Get("total_value") == "" {
		q.Set("total_value", "0.000")
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache, must-revalidate")
	name := a.site.Name
	if name == "" {
		name = defaultSiteConfig().Name
	}
	iconURL := a.shareIconURL(r)
	if iconURL != "" {
		iconURL = a.embeddedShareIcon(r, iconURL)
	}
	if iconURL == "" {
		if data, err := os.ReadFile(filepath.Join(a.staticDir, "static", "brand-icon.svg")); err == nil {
			iconURL = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(data)
		}
	}
	_, _ = io.WriteString(w, shareSVG(q, name, iconURL))
}

func (a *app) embeddedShareIcon(r *http.Request, iconURL string) string {
	data, mediaType, ok := a.fetchSiteIcon(r, iconURL)
	if !ok {
		return ""
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (a *app) siteIcon(w http.ResponseWriter, r *http.Request) {
	if a.site.IconURL != "" {
		if data, mediaType, ok := a.fetchSiteIcon(r, a.resolvedSiteIconURL(r)); ok {
			w.Header().Set("Content-Type", mediaType)
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(data)
			return
		}
	}
	data, err := os.ReadFile(filepath.Join(a.staticDir, "static", "brand-icon.svg"))
	if err != nil {
		http.Error(w, "site icon unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

func (a *app) fetchSiteIcon(r *http.Request, iconURL string) ([]byte, string, bool) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, iconURL, nil)
	if err != nil {
		return nil, "", false
	}
	client := a.client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", false
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 1<<20 {
		return nil, "", false
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" && mediaType != "image/webp" && mediaType != "image/svg+xml") {
		return nil, "", false
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) && mediaType == "image/png" {
		return nil, "", false
	}
	return data, mediaType, true
}

func (a *app) shareIconURL(r *http.Request) string {
	return a.resolvedSiteIconURL(r)
}

func (a *app) resolvedSiteIconURL(r *http.Request) string {
	iconURL := a.site.IconURL
	if iconURL == "" {
		return ""
	}
	if strings.HasPrefix(iconURL, "/") {
		baseURL := a.site.URL
		if baseURL == "" {
			baseURL = requestBaseURL(r)
		}
		return strings.TrimRight(baseURL, "/") + iconURL
	}
	return iconURL
}

func shareSVG(q url.Values, siteName, iconURL string) string {
	svg := shareSVGBase(q, siteName, iconURL)
	if q.Get("transaction_amount") == "" || q.Get("premium") == "" {
		return svg
	}
	premiumColor, premiumLabel := "#087e70", "折价"
	if premium, err := strconv.ParseFloat(q.Get("premium"), 64); err == nil {
		if premium > 0 {
			premiumColor, premiumLabel = "#c15c24", "溢价"
		} else if premium == 0 {
			premiumLabel = "平价"
		}
	}
	premiumAmount := q.Get("premium")
	if strings.HasPrefix(premiumAmount, "-") || premiumAmount == "0.000" {
		premiumAmount += " 元"
	} else {
		premiumAmount = "+" + premiumAmount + " 元"
	}
	premiumBasis := "标准估值"
	if q.Get("custom_exchange_rate") != "" && q.Get("custom_exchange_rate") != q.Get("exchange_rate") {
		premiumBasis = "自定义估值"
	}
	transactionMarkup := fmt.Sprintf(`<g font-family="Arial, sans-serif"><text x="58" y="389" font-size="10" font-weight="700" fill="#87948b">交易金额</text><text x="58" y="412" font-size="14" font-weight="700" fill="#182923">%s 元</text><text x="275" y="389" font-size="10" font-weight="700" fill="#87948b">溢价值 / %s（%s）</text><text x="275" y="412" font-size="14" font-weight="700" fill="%s">%s</text></g>`, escapeXML(q.Get("transaction_amount")), premiumLabel, premiumBasis, premiumColor, escapeXML(premiumAmount))
	svg = strings.Replace(svg, `</g><path d="M54 389h612"`, `</g>`+transactionMarkup+`<path d="M54 439h612"`, 1)
	return strings.NewReplacer(
		`height="460" viewBox="0 0 720 460"`, `height="500" viewBox="0 0 720 500"`,
		`<rect width="720" height="460" fill=`, `<rect width="720" height="500" fill=`,
		`width="672" height="412"`, `width="672" height="452"`,
		`M38 24h2v412h-2z`, `M38 24h2v452h-2z`,
		`y="411"`, `y="461"`,
	).Replace(svg)
}

func shareSVGBase(q url.Values, siteName, iconURL string) string {
	currencyNames := map[string]string{"CNY": "人民币", "USD": "美元", "GBP": "英镑", "EUR": "欧元", "JPY": "日元", "KRW": "韩元", "HKD": "港元", "TWD": "新台币", "CAD": "加拿大元", "SGD": "新加坡元", "AUD": "澳大利亚元"}
	currencyName := currencyNames[q.Get("currency_code")]
	if currencyName == "" {
		currencyName = q.Get("currency_code")
	}
	cycleNames := map[string]string{"monthly": "月付", "quarterly": "季付", "semiannually": "半年付", "annually": "年付", "biennially": "两年付", "triennially": "三年付", "quinquennially": "五年付"}
	cycleName := cycleNames[q.Get("cycle")]
	if cycleName == "" {
		cycleName = q.Get("cycle")
	}
	tradeDate := displayDate(q.Get("trade_date"))
	expiryDate := displayDate(q.Get("expiry_date"))
	createdDate := displayDate(q.Get("created_date"))
	if createdDate == "" {
		createdDate = tradeDate
	}
	remainValue := q.Get("remain_value")
	valueFontSize := 54
	if len([]rune(remainValue)) > 11 {
		valueFontSize = 38
	} else if len([]rune(remainValue)) > 8 {
		valueFontSize = 45
	}
	siteFontSize := 20
	if len([]rune(siteName)) > 30 {
		siteFontSize = 14
	}
	if len([]rune(siteName)) > 48 {
		siteFontSize = 11
	}
	customPriceLabel, customPrice := "估值方式", "自然日折算"
	if customRate := q.Get("custom_exchange_rate"); customRate != "" && customRate != q.Get("exchange_rate") {
		customPriceLabel = "自定义价 / " + customRate
		customPrice = q.Get("custom_remain_value") + " 元"
	}
	iconFrame := `<rect x="54" y="46" width="44" height="44" rx="9" fill="#182923"/><path d="M65 59h22v17H65zM69 55v4m14-4v4m-14 21v4m14-4v4" fill="none" stroke="#b8e65c" stroke-width="2" stroke-linecap="round"/>`
	iconImage := `<g transform="translate(54 46) scale(0.458333)"><rect width="96" height="96" rx="20" fill="#182923"/><path d="M22 25h52v16H22zm0 30h52v16H22z" fill="#b8e65c"/><circle cx="65" cy="33" r="3" fill="#182923"/><circle cx="65" cy="63" r="3" fill="#182923"/></g>`
	if iconURL != "" {
		iconFrame = ""
		iconImage = `<image href="` + escapeXML(iconURL) + `" x="58" y="50" width="36" height="36" preserveAspectRatio="xMidYMid meet" clip-path="url(#siteIconClip)"/>`
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="720" height="460" viewBox="0 0 720 460" role="img" aria-label="%s VPS 资产快照"><defs><clipPath id="siteIconClip"><rect x="58" y="50" width="36" height="36" rx="7"/></clipPath></defs><rect width="720" height="460" fill="#e8eee5"/><rect x="24" y="24" width="672" height="412" rx="14" fill="#fbfcf9" stroke="#d5dfd3"/><path d="M38 24h2v412h-2z" fill="#b8e65c"/>%s%s<text x="112" y="65" font-family="Arial, sans-serif" font-size="%d" font-weight="700" fill="#182923">%s</text><text x="112" y="84" font-family="Arial, sans-serif" font-size="10" font-weight="700" letter-spacing="1.1" fill="#718078">VPS ASSET SNAPSHOT</text><text x="662" y="59" text-anchor="end" font-family="Arial, sans-serif" font-size="11" fill="#718078">生成于 %s</text><path d="M54 108h612" stroke="#dce4da"/><text x="58" y="139" font-family="Arial, sans-serif" font-size="11" font-weight="700" letter-spacing=".7" fill="#718078">剩余价值 / REMAINING VALUE</text><text x="58" y="202" font-family="Arial, sans-serif" font-size="%d" font-weight="700" fill="#087e70">%s<tspan font-size="19" font-weight="500"> 元</tspan></text><rect x="501" y="126" width="163" height="108" rx="8" fill="#182923"/><path d="M519 144h22" stroke="#b8e65c" stroke-width="3" stroke-linecap="round"/><text x="519" y="164" font-family="Arial, sans-serif" font-size="10" font-weight="700" letter-spacing=".8" fill="#c3cec4">剩余天数</text><text x="519" y="211" font-family="Arial, sans-serif" font-size="40" font-weight="700" fill="#fff">%s<tspan font-size="15" font-weight="500"> 天</tspan></text><text x="58" y="231" font-family="Arial, sans-serif" font-size="12" fill="#718078">总价值 %s 元</text><path d="M54 252h612" stroke="#dce4da"/><g font-family="Arial, sans-serif"><text x="58" y="280" font-size="10" font-weight="700" fill="#87948b">交易日期</text><text x="58" y="302" font-size="14" font-weight="600" fill="#182923">%s</text><text x="275" y="280" font-size="10" font-weight="700" fill="#87948b">付款方案</text><text x="275" y="302" font-size="14" font-weight="600" fill="#182923">%s · %s</text><text x="500" y="280" font-size="10" font-weight="700" fill="#87948b">到期时间</text><text x="500" y="302" font-size="14" font-weight="600" fill="#182923">%s</text><text x="58" y="337" font-size="10" font-weight="700" fill="#87948b">续费价格</text><text x="58" y="359" font-size="13" font-weight="600" fill="#182923">%s</text><text x="275" y="337" font-size="10" font-weight="700" fill="#87948b">折算汇率</text><text x="275" y="359" font-size="13" font-weight="600" fill="#182923">%s</text><text x="500" y="337" font-size="10" font-weight="700" fill="#87948b">%s</text><text x="500" y="359" font-size="11" font-weight="700" fill="#588a12">%s</text></g><path d="M54 389h612" stroke="#dce4da"/><text x="58" y="411" font-family="Arial, sans-serif" font-size="10" font-weight="700" letter-spacing=".7" fill="#87948b">VPS / SERVER VALUE RECORD</text><text x="662" y="411" text-anchor="end" font-family="Arial, sans-serif" font-size="11" fill="#718078">由 %s 提供</text></svg>`, escapeXML(siteName), iconFrame, iconImage, siteFontSize, escapeXML(siteName), escapeXML(createdDate), valueFontSize, escapeXML(remainValue), escapeXML(q.Get("remain_days")), escapeXML(q.Get("total_value")), escapeXML(tradeDate), escapeXML(cycleName), escapeXML(currencyName), escapeXML(expiryDate), escapeXML(q.Get("renewal")), escapeXML(q.Get("exchange_rate")), escapeXML(customPriceLabel), escapeXML(customPrice), escapeXML(siteName))
}

func requestBaseURL(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		return forwarded + "://" + r.Host
	}
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func displayDate(value string) string {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return parsed.Format("2006/01/02")
}

func rateProviderConfig() (string, string, error) {
	provider := strings.ToLower(strings.TrimSpace(env("RATE_PROVIDER", "frankfurter")))
	if endpoint := strings.TrimSpace(os.Getenv("RATES_URL")); endpoint != "" {
		if provider != "frankfurter" && provider != "exchangerate-api" {
			return "", "", fmt.Errorf("unsupported RATE_PROVIDER %q", provider)
		}
		return provider, endpoint, nil
	}
	switch provider {
	case "frankfurter":
		query := url.Values{
			"base":   {"CNY"},
			"quotes": {strings.Join(currencies[1:], ",")},
		}
		return provider, "https://api.frankfurter.dev/v2/rates?" + query.Encode(), nil
	case "exchangerate-api":
		if key := strings.TrimSpace(os.Getenv("EXCHANGERATE_API_KEY")); key != "" {
			return provider, "https://v6.exchangerate-api.com/v6/" + url.PathEscape(key) + "/latest/CNY", nil
		}
		return provider, "https://open.er-api.com/v6/latest/CNY", nil
	default:
		return "", "", fmt.Errorf("unsupported RATE_PROVIDER %q; use frankfurter or exchangerate-api", provider)
	}
}

func escapeXML(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(value)
}

func positiveNumber(value, label string) (float64, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number <= 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s必须是大于零的有效数字", label)
	}
	return number, nil
}

func nonNegativeNumber(value, label string) (float64, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s必须是大于或等于零的有效数字", label)
	}
	return number, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"status": "error", "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}
