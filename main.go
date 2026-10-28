package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var currencies = []string{"CNY", "USD", "GBP", "EUR", "JPY", "KRW", "HKD", "TWD", "CAD", "SGD", "AUD"}

type app struct {
	client    *http.Client
	provider  string
	rateURL   string
	staticDir string
	site      siteConfig
	mu        sync.Mutex
	cached    rateResult
	cacheAt   time.Time
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
	service := &app{
		client:    &http.Client{Timeout: 8 * time.Second},
		provider:  provider,
		rateURL:   rateURL,
		staticDir: env("STATIC_DIR", "."),
		site:      site,
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
	icon := ""
	if a.site.IconURL != "" {
		icon = `<link rel="icon" href="` + html.EscapeString(a.site.IconURL) + `">`
	}
	content = []byte(strings.NewReplacer(
		"__SITE_NAME__", html.EscapeString(a.site.Name),
		"__SITE_DESCRIPTION__", html.EscapeString(a.site.Description),
		"__SITE_KEYWORDS__", html.EscapeString(a.site.Keywords),
		"__SITE_URL__", html.EscapeString(canonical),
		"__SITE_ICON__", icon,
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
		if !isAbsolute && !isLocalPath {
			return siteConfig{}, errors.New("SITE_ICON_URL must be an http(s) URL or an absolute site path")
		}
		config.IconURL = value
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
	result.SharePic = a.makeShareURL(r, input, result)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": result})
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
	currencyName := map[string]string{"CNY": "人民币", "USD": "美元", "GBP": "英镑", "EUR": "欧元", "JPY": "日元", "KRW": "韩元", "HKD": "港元", "TWD": "新台币", "CAD": "加拿大元", "SGD": "新加坡元", "AUD": "澳大利亚元"}[input.CurrencyCode]
	base := resultData{
		TradeDate: trade.Format("2006-01-02"), ExchangeRate: fmt.Sprintf("%.3f", rate),
		Renewal: fmt.Sprintf("%.2f %s / 年", amount*12/float64(months), currencyName), RemainDays: remainDays,
		ExpiryDate: expiry.Format("2006-01-02"), RemainValue: fmt.Sprintf("%.3f", remain),
		TotalValue: fmt.Sprintf("%.3f", total), CustomRemainValue: fmt.Sprintf("%.3f", customRemain),
		CustomExchangeRate: fmt.Sprintf("%.3f", customRate),
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
		"total_value": {result.TotalValue}, "created_date": {time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")},
	}
	id := base64.RawURLEncoding.EncodeToString([]byte(values.Encode()))
	return strings.TrimRight(baseURL, "/") + "/share/" + id + ".svg"
}

func (a *app) share(w http.ResponseWriter, r *http.Request) {
	// Keep the old query endpoint working for links generated by earlier builds.
	a.renderShare(w, r.URL.Query())
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
	a.renderShare(w, values)
}

func (a *app) renderShare(w http.ResponseWriter, q url.Values) {
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
	w.Header().Set("Cache-Control", "public, max-age=86400")
	name := a.site.Name
	if name == "" {
		name = defaultSiteConfig().Name
	}
	_, _ = io.WriteString(w, shareSVG(q, name))
}

func shareSVG(q url.Values, siteName string) string {
	currencyNames := map[string]string{"CNY": "人民币", "USD": "美元", "GBP": "英镑", "EUR": "欧元", "JPY": "日元", "KRW": "韩元", "HKD": "港元", "TWD": "新台币", "CAD": "加拿大元", "SGD": "新加坡元", "AUD": "澳大利亚元"}
	currencyName := currencyNames[q.Get("currency_code")]
	if currencyName == "" {
		currencyName = q.Get("currency_code")
	}
	tradeDate := displayDate(q.Get("trade_date"))
	expiryDate := displayDate(q.Get("expiry_date"))
	createdDate := displayDate(q.Get("created_date"))
	if createdDate == "" {
		createdDate = tradeDate
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 570 400" style="max-width: 1004px; width: 100%%; height: auto;" width="1004"><defs><linearGradient id="grad1" x1="0%%" y1="0%%" x2="100%%" y2="100%%"><stop offset="0%%" style="stop-color:#ffc8c7;stop-opacity:1"/><stop offset="12%%" style="stop-color:#ffccdf;stop-opacity:1"/><stop offset="28%%" style="stop-color:#ffccf5;stop-opacity:1"/><stop offset="43%%" style="stop-color:#f3d1ff;stop-opacity:1"/><stop offset="56%%" style="stop-color:#ddd1ff;stop-opacity:1"/><stop offset="70%%" style="stop-color:#d1d6ff;stop-opacity:1"/><stop offset="84%%" style="stop-color:#d6e5ff;stop-opacity:1"/><stop offset="100%%" style="stop-color:#d6e9ff;stop-opacity:1"/></linearGradient></defs> <rect x="0" y="0" width="570" height="400" rx="8" fill="url(#grad1)"/><rect x="55" y="25" width="460" height="335" rx="8" fill="rgba(255, 255, 255, 0.3)" stroke="rgba(255, 255, 255, 0.3)" stroke-width="1"/><rect x="110" y="130" width="350" height="200" fill="#FFF" fill-opacity="0.5" stroke="#CCC" stroke-width="1"/><rect x="110" y="130" width="90" height="40" fill="#f6f8fa" fill-opacity="0.5"/><rect x="200" y="130" width="260" height="40" fill="#f6f8fa" fill-opacity="0.5"/><rect x="110" y="170" width="90" height="40" fill="#ffffff" fill-opacity="0.5"/><rect x="200" y="170" width="260" height="40" fill="#ffffff" fill-opacity="0.5"/><rect x="110" y="210" width="90" height="40" fill="#f6f8fa" fill-opacity="0.5"/><rect x="200" y="210" width="260" height="40" fill="#f6f8fa" fill-opacity="0.5"/><rect x="110" y="250" width="90" height="40" fill="#ffffff" fill-opacity="0.5"/><rect x="200" y="250" width="260" height="40" fill="#ffffff" fill-opacity="0.5"/><rect x="110" y="290" width="90" height="40" fill="#f6f8fa" fill-opacity="0.5"/><rect x="200" y="290" width="260" height="40" fill="#f6f8fa" fill-opacity="0.5"/><text x="285" y="85" font-size="22" font-weight="bold" fill="#333" text-anchor="middle">%s</text><text x="350" y="120" font-size="14" fill="#767676">%s 创建</text><line x1="200" y1="130" x2="200" y2="330" stroke="#CCC" stroke-width="1"/><line x1="110" y1="170" x2="460" y2="170" stroke="#CCC" stroke-width="1"/><line x1="110" y1="210" x2="460" y2="210" stroke="#CCC" stroke-width="1"/><line x1="110" y1="250" x2="460" y2="250" stroke="#CCC" stroke-width="1"/><line x1="110" y1="290" x2="460" y2="290" stroke="#CCC" stroke-width="1"/><g font-size="16" fill="#333333" font-family="Arial, sans-serif"><text x="120" y="155" fill="#767676">交易日期：</text><text x="220" y="155">%s</text><text x="120" y="195" fill="#767676">外币汇率：</text><text x="220" y="195">%s</text><text x="120" y="235" fill="#767676">续费价格：</text><text x="220" y="235">%s</text><text x="120" y="275" fill="#767676">剩余天数：</text><text x="220" y="275" fill="green">%s 天</text><text x="270" y="275" font-size="14" fill="#b8b8b8">(于 %s 过期)</text><text x="120" y="315" fill="#767676">剩余价值：</text><text x="220" y="315" fill="red">%s 元</text><text x="300" y="315" font-size="14" fill="#b8b8b8">(总 %s 元)</text></g><text x="515" y="385" font-size="14" fill="#767676" text-anchor="end">由 <tspan fill="#767676">%s</tspan> 提供</text></svg>`, escapeXML(siteName), escapeXML(createdDate), escapeXML(tradeDate), escapeXML(q.Get("exchange_rate")), escapeXML(q.Get("renewal")), escapeXML(q.Get("remain_days")), escapeXML(expiryDate), escapeXML(q.Get("remain_value")), escapeXML(q.Get("total_value")), escapeXML(siteName))
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
