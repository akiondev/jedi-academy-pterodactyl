package antivpn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type providerGate struct {
	mu          sync.Mutex
	lastRequest time.Time
	minInterval time.Duration
}

func (g *providerGate) Wait(ctx context.Context) error {
	if g == nil || g.minInterval <= 0 {
		return nil
	}

	g.mu.Lock()
	waitFor := time.Until(g.lastRequest.Add(g.minInterval))
	if waitFor < 0 {
		waitFor = 0
	}
	g.lastRequest = time.Now().UTC().Add(waitFor)
	g.mu.Unlock()

	if waitFor == 0 {
		return nil
	}

	timer := time.NewTimer(waitFor)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type providerBase struct {
	name    string
	client  *http.Client
	gate    *providerGate
	retries int
	logger  *slog.Logger
}

func (b providerBase) Name() string {
	return b.name
}

func buildProviders(cfg Config, logger *slog.Logger) []Provider {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: cfg.Timeout,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	newBase := func(name string) providerBase {
		return providerBase{
			name: name,
			client: &http.Client{
				Timeout:   cfg.Timeout,
				Transport: transport,
			},
			gate: &providerGate{
				minInterval: cfg.ProviderMinInterval,
			},
			retries: cfg.RetryCount,
			logger:  logger,
		}
	}

	return []Provider{
		&proxyCheckProvider{base: newBase("proxycheck.io"), apiKey: cfg.ProxyCheckAPIKey},
		&ipapiISProvider{base: newBase("ipapi.is"), apiKey: cfg.IPAPIISAPIKey},
		&ipHubProvider{base: newBase("IPHub"), apiKey: cfg.IPHubAPIKey},
		&vpnAPIIoProvider{base: newBase("vpnapi.io"), apiKey: cfg.VPNAPIIoAPIKey},
	}
}

func getJSON(ctx context.Context, base providerBase, endpoint string, headers map[string]string, target any) (int, error) {
	var lastErr error
	var statusCode int

	for attempt := 0; attempt <= base.retries; attempt++ {
		if err := base.gate.Wait(ctx); err != nil {
			return 0, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "taystjk-antivpn/1.0")
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := base.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < base.retries {
				time.Sleep(150 * time.Millisecond)
				continue
			}
			return 0, lastErr
		}

		statusCode = resp.StatusCode
		body := io.LimitReader(resp.Body, 1<<20)
		decodeErr := json.NewDecoder(body).Decode(target)
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			if attempt < base.retries {
				time.Sleep(150 * time.Millisecond)
				continue
			}
			return statusCode, lastErr
		}

		if resp.StatusCode >= 400 {
			return statusCode, fmt.Errorf("http %d", resp.StatusCode)
		}
		if decodeErr != nil {
			lastErr = decodeErr
			if attempt < base.retries {
				time.Sleep(150 * time.Millisecond)
				continue
			}
			return statusCode, decodeErr
		}

		return statusCode, nil
	}

	return statusCode, lastErr
}

type proxyCheckProvider struct {
	base  providerBase
	apiKey string
}

func (p *proxyCheckProvider) Name() string {
	return p.base.Name()
}

func (p *proxyCheckProvider) Lookup(ctx context.Context, ip netip.Addr) ProviderResult {
	start := time.Now()
	result := ProviderResult{
		Provider: p.Name(),
	}

	query := url.Values{}
	query.Set("vpn", "2")
	query.Set("asn", "1")
	query.Set("risk", "1")
	query.Set("days", "30")
	if p.apiKey != "" {
		query.Set("key", p.apiKey)
	}

	endpoint := fmt.Sprintf("https://proxycheck.io/v2/%s?%s", url.PathEscape(ip.String()), query.Encode())

	var payload map[string]json.RawMessage
	status, err := getJSON(ctx, p.base, endpoint, nil, &payload)
	result.HTTPStatus = status
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.Summary = "lookup failed"
		return result
	}

	result.Success = true

	if statusValue := anyString(payload["status"]); statusValue != "" && !strings.EqualFold(statusValue, "ok") && !strings.EqualFold(statusValue, "warning") {
		result.Success = false
		result.Error = "provider returned non-ok status"
		result.Summary = fmt.Sprintf("status=%s", statusValue)
		return result
	}

	raw, ok := payload[ip.String()]
	if !ok {
		result.Summary = "no matching address payload returned"
		return result
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Summary = "invalid response payload"
		return result
	}

	proxyValue := strings.ToLower(anyValueString(body["proxy"]))
	typeValue := strings.ToLower(anyValueString(body["type"]))
	riskValue := anyValueInt(body["risk"])
	operator := anyValueString(body["provider"])

	summaryParts := []string{}
	if proxyValue != "" {
		summaryParts = append(summaryParts, "proxy="+proxyValue)
	}
	if typeValue != "" {
		summaryParts = append(summaryParts, "type="+typeValue)
	}
	if riskValue > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("risk=%d", riskValue))
	}
	if operator != "" {
		summaryParts = append(summaryParts, "provider="+operator)
	}
	if len(summaryParts) == 0 {
		summaryParts = append(summaryParts, "no vpn or hosting signal")
	}
	result.Summary = strings.Join(summaryParts, " ")

	recognized := false
	if proxyValue == "yes" && strings.Contains(typeValue, "vpn") {
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "vpn",
			Strength: "strong",
			Reason:   "proxycheck.io classified the IP as VPN",
			Weight:   80,
		})
		recognized = true
	}
	if proxyValue == "yes" && (strings.Contains(typeValue, "hosting") || strings.Contains(typeValue, "datacenter")) {
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "hosting",
			Strength: "medium",
			Reason:   "proxycheck.io classified the IP as hosting-backed",
			Weight:   55,
		})
		recognized = true
	}
	if recognized && riskValue >= 85 {
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "risk",
			Strength: "medium",
			Reason:   fmt.Sprintf("proxycheck.io reported elevated risk %d", riskValue),
			Weight:   15,
		})
	}

	return result
}

type ipapiISProvider struct {
	base  providerBase
	apiKey string
}

func (p *ipapiISProvider) Name() string {
	return p.base.Name()
}

type ipapiISResponse struct {
	Error        string `json:"error"`
	IP           string `json:"ip"`
	IsVPN        bool   `json:"is_vpn"`
	IsDatacenter bool   `json:"is_datacenter"`
	Company      struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"company"`
	ASN struct {
		Org  string `json:"org"`
		Type string `json:"type"`
	} `json:"asn"`
	VPN struct {
		Service string `json:"service"`
		Type    string `json:"type"`
		Region  string `json:"region"`
	} `json:"vpn"`
	Datacenter struct {
		Datacenter string `json:"datacenter"`
		Network    string `json:"network"`
	} `json:"datacenter"`
}

func (p *ipapiISProvider) Lookup(ctx context.Context, ip netip.Addr) ProviderResult {
	start := time.Now()
	result := ProviderResult{
		Provider: p.Name(),
	}

	query := url.Values{}
	query.Set("q", ip.String())
	if p.apiKey != "" {
		query.Set("key", p.apiKey)
	}

	endpoint := "https://api.ipapi.is/?" + query.Encode()
	var payload ipapiISResponse
	status, err := getJSON(ctx, p.base, endpoint, nil, &payload)
	result.HTTPStatus = status
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.Summary = "lookup failed"
		return result
	}

	if payload.Error != "" {
		result.Error = payload.Error
		result.Summary = payload.Error
		return result
	}

	result.Success = true
	summaryParts := []string{}
	if payload.IsVPN {
		service := strings.TrimSpace(payload.VPN.Service)
		if service == "" {
			service = "unknown"
		}
		summaryParts = append(summaryParts, "vpn="+service)
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "vpn",
			Strength: "strong",
			Reason:   fmt.Sprintf("ipapi.is detected a VPN exit node (%s)", service),
			Weight:   70,
		})
	}
	if payload.IsDatacenter {
		name := strings.TrimSpace(payload.Datacenter.Datacenter)
		if name == "" {
			name = payload.Company.Name
		}
		if name == "" {
			name = "unknown datacenter"
		}
		summaryParts = append(summaryParts, "datacenter="+name)
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "hosting",
			Strength: "medium",
			Reason:   fmt.Sprintf("ipapi.is marked the IP as datacenter/hosting (%s)", name),
			Weight:   20,
		})
	}
	if strings.EqualFold(payload.Company.Type, "hosting") || strings.EqualFold(payload.ASN.Type, "hosting") {
		owner := strings.TrimSpace(payload.Company.Name)
		if owner == "" {
			owner = strings.TrimSpace(payload.ASN.Org)
		}
		if owner == "" {
			owner = "hosting operator"
		}
		summaryParts = append(summaryParts, "company_type=hosting")
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "hosting",
			Strength: "weak",
			Reason:   fmt.Sprintf("ipapi.is WHOIS ownership is hosting-oriented (%s)", owner),
			Weight:   10,
		})
	}

	if len(summaryParts) == 0 {
		summaryParts = append(summaryParts, "no vpn or hosting signal")
	}
	result.Summary = strings.Join(summaryParts, " ")
	return result
}

type ipHubProvider struct {
	base  providerBase
	apiKey string
}

func (p *ipHubProvider) Name() string {
	return p.base.Name()
}

type ipHubResponse struct {
	IP          string `json:"ip"`
	Block       int    `json:"block"`
	ISP         string `json:"isp"`
	CountryCode string `json:"countryCode"`
	CountryName string `json:"countryName"`
}

func (p *ipHubProvider) Lookup(ctx context.Context, ip netip.Addr) ProviderResult {
	start := time.Now()
	result := ProviderResult{
		Provider: p.Name(),
	}

	if p.apiKey == "" {
		result.Summary = "provider disabled because no API key is configured"
		return result
	}

	endpoint := fmt.Sprintf("http://v2.api.iphub.info/ip/%s", url.PathEscape(ip.String()))
	var payload ipHubResponse
	status, err := getJSON(ctx, p.base, endpoint, map[string]string{"X-Key": p.apiKey}, &payload)
	result.HTTPStatus = status
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.Summary = "lookup failed"
		return result
	}

	result.Success = true
	result.Summary = fmt.Sprintf("block=%d isp=%s", payload.Block, strings.TrimSpace(payload.ISP))

	switch payload.Block {
	case 1:
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "non-residential",
			Strength: "medium",
			Reason:   "IPHub classified the IP as non-residential",
			Weight:   35,
		})
	case 2:
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "mixed",
			Strength: "weak",
			Reason:   "IPHub classified the IP as mixed residential and non-residential",
			Weight:   15,
		})
	}

	return result
}

type vpnAPIIoProvider struct {
	base  providerBase
	apiKey string
}

func (p *vpnAPIIoProvider) Name() string {
	return p.base.Name()
}

type vpnAPIIoResponse struct {
	IP       string `json:"ip"`
	Security struct {
		VPN bool `json:"vpn"`
	} `json:"security"`
	Network struct {
		AutonomousSystemOrganization string `json:"autonomous_system_organization"`
	} `json:"network"`
}

func (p *vpnAPIIoProvider) Lookup(ctx context.Context, ip netip.Addr) ProviderResult {
	start := time.Now()
	result := ProviderResult{
		Provider: p.Name(),
	}

	if p.apiKey == "" {
		result.Summary = "provider disabled because no API key is configured"
		return result
	}

	query := url.Values{}
	query.Set("key", p.apiKey)
	endpoint := fmt.Sprintf("https://vpnapi.io/api/%s?%s", url.PathEscape(ip.String()), query.Encode())

	var payload vpnAPIIoResponse
	status, err := getJSON(ctx, p.base, endpoint, nil, &payload)
	result.HTTPStatus = status
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.Summary = "lookup failed"
		return result
	}

	result.Success = true
	if payload.Security.VPN {
		operator := strings.TrimSpace(payload.Network.AutonomousSystemOrganization)
		if operator == "" {
			operator = "unknown operator"
		}
		result.Signals = append(result.Signals, Signal{
			Provider: p.Name(),
			Category: "vpn",
			Strength: "strong",
			Reason:   fmt.Sprintf("vpnapi.io detected a VPN-backed network (%s)", operator),
			Weight:   55,
		})
		result.Summary = fmt.Sprintf("vpn=true operator=%s", operator)
		return result
	}

	result.Summary = "no vpn signal"
	return result
}

func anyString(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return anyValueString(value)
}

func anyValueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func anyValueInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}
