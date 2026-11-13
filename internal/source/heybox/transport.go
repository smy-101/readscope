package heybox

// transport.go — the shared transport every heybox request rides
// (ticket 01), the zhihu transport discipline applied to an anonymous
// signed host: one place owns the measured request shape (desktop
// Chrome UA, the site Referer, the 999.0.3 web parameter set, the
// persistent device_id), the hkey/_time/nonce signature, the
// risk-control sentinel (captcha / 429 / version gate → one
// "try later" error, cache untouched), and the inter-page politeness
// delay the smoke ran with. A cookie hook is deliberately absent —
// login is out of scope by ADR #16, and the shape swap
// (shapeWebV3 ↔ shapeWebV4) is the stop-loss ladder's first rung.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fetchTimeout bounds one HTTP round trip; parsing is local.
const fetchTimeout = 30 * time.Second

// maxBodyBytes caps one response body (the fattest legitimate payload
// is a topic-feeds page at ~136KB; everything past this is a
// runaway).
const maxBodyBytes = 8 << 20

// defaultMinGap is the politeness delay between two requests riding
// one transport — the smoke's 1.5s floor, which drew zero 429s across
// ~30 requests. The cookie-gated M4 host needed no such delay; the
// heybox web endpoints rate-shape by request frequency, so the delay
// rides here, context-cancellable, rather than as sleeps scattered
// through callers.
const defaultMinGap = 1500 * time.Millisecond

// ErrTryLater is the risk-control family's one error (the spec's
// 风控语义): show_captcha, HTTP 429, or a version-gate refusal. The
// current refresh failed; nothing else changes — the cache keeps
// serving, no retry is scheduled, and there is no re-login to point
// at (an anonymous source). The UI maps it to the 「稍后再试」
// status line.
var ErrTryLater = errors.New("heybox: risk control refused — try later (cache unaffected)")

// Transport is one base URL + device fingerprint + client shape
// behind every heybox GET. Cheap to build (the source constructs one
// per refresh — the zhihu pattern); safe for concurrent use (the
// only post-construction state is the politeness ledger, mutex-
// guarded; races cost courtesy, never correctness).
type Transport struct {
	base     string
	deviceID string
	shape    clientShape // version parameter + its bound signer
	client   *http.Client
	minGap   time.Duration // politeness floor between this transport's requests

	// mu guards lastReq (the only post-construction state — the
	// politeness ledger). The gap is advisory: a race costs courtesy,
	// never correctness, so the lock is held for two instants only.
	mu      sync.Mutex
	lastReq time.Time // zero: the first request rides without delay
}

// TransportOpt tweaks one transport at construction; tests are the
// only user today (a zero delay, a fake's client, a pinned shape).
type TransportOpt func(*Transport)

// WithClient pins the HTTP client (tests inject the fake's client).
func WithClient(c *http.Client) TransportOpt { return func(t *Transport) { t.client = c } }

// WithMinGap pins the politeness delay (tests pass 0 to run flat out,
// or a few ms to observe the gap).
func WithMinGap(d time.Duration) TransportOpt { return func(t *Transport) { t.minGap = d } }

// WithShape pins the client shape (the stop-loss switch's seam: the
// v4 pair when the version gate bites).
func WithShape(s clientShape) TransportOpt { return func(t *Transport) { t.shape = s } }

// NewTransport builds the production transport: default shapeWebV3,
// default politeness gap, default client.
func NewTransport(base, deviceID string, opts ...TransportOpt) *Transport {
	t := &Transport{
		base:     base,
		deviceID: deviceID,
		shape:    shapeWebV3,
		minGap:   defaultMinGap,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Response is one raw outcome. Body rides along even on refusals —
// the error document is the evidence a probe (or a test) wants to
// name — and Result is the envelope's peeled result payload, ready
// for the caller's own shape (nil on non-ok answers).
type Response struct {
	Status int
	Body   []byte
	Result json.RawMessage
	Err    error
}

// envelope is every heybox endpoint's answer wrapper: {"status":
// "ok"|"failed"|"show_captcha", "msg": …, "result": …}. The result
// rides raw — each caller unmarshals its own shape.
type envelope struct {
	Status string          `json:"status"`
	Msg    string          `json:"msg"`
	Result json.RawMessage `json:"result"`
}

// Get performs one signed GET: the endpoint's own params first, then
// the common web set (version, os_type, x_app…, heybox_id, the
// persistent device_id), then the signature triple — hkey/_time/
// nonce, computed over the canonical path alone. The classification
// at the end is the sentinel: HTTP 429, status show_captcha and the
// version-gate message family all fold into ErrTryLater; any other
// non-ok answer is a plain error carrying the server's own msg; a
// 200/ok rides home for the caller to parse.
func (t *Transport) Get(ctx context.Context, name, path string, extra url.Values) Response {
	if err := t.wait(ctx); err != nil {
		return Response{Err: fmt.Errorf("%s: %w", name, err)}
	}
	t.mu.Lock()
	t.lastReq = time.Now()
	t.mu.Unlock()
	params := url.Values{}
	for k, vs := range extra {
		params[k] = vs
	}
	// The common web set is the smoke's 精简集 verbatim — os_type,
	// version, x_app, x_client_type, x_os_type — plus heybox_id=-1 (the
	// anonymous spelling every reference client sends) and the
	// persistent device fingerprint (spec user story 24; the smoke's
	// P14 ran green without one, the spec pins it for the feed's
	// device-keyed personalization). Nothing else: the signature binds
	// to the version string, so the shape stays exactly as measured.
	for k, v := range map[string]string{
		"os_type":       "web",
		"version":       t.shape.version,
		"x_app":         "heybox_website",
		"x_client_type": "web",
		"x_os_type":     "Windows",
		"heybox_id":     "-1",
		"device_id":     t.deviceID,
	} {
		params.Set(k, v)
	}
	hkey, nonce, ts, err := signTriple(t.shape, path, time.Now())
	if err != nil {
		return Response{Err: fmt.Errorf("%s: %w", name, err)}
	}
	params.Set("hkey", hkey)
	params.Set("_time", strconv.FormatInt(ts, 10))
	params.Set("nonce", nonce)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+path+"?"+params.Encode(), nil)
	if err != nil {
		return Response{Err: fmt.Errorf("%s: %w", name, err)}
	}
	h := req.Header
	h.Set("User-Agent", DesktopUA)
	h.Set("Accept", "application/json")
	h.Set("Accept-Language", "zh-CN,zh;q=0.9")
	h.Set("Referer", WebReferer)
	client := t.client
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{Err: fmt.Errorf("%s: %w", name, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Response{Status: resp.StatusCode, Err: fmt.Errorf("%s: %w", name, err)}
	}
	res := Response{Status: resp.StatusCode, Body: body}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		res.Err = fmt.Errorf("%s: %w (HTTP 429)", name, ErrTryLater)
		return res
	case resp.StatusCode != http.StatusOK:
		res.Err = fmt.Errorf("%s: HTTP %d", name, resp.StatusCode)
		return res
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		res.Err = fmt.Errorf("%s: not heybox JSON: %w", name, err)
		return res
	}
	switch {
	case env.Status == "ok":
		res.Result = env.Result
		return res
	case env.Status == "show_captcha" || isVersionGate(env.Msg):
		// The risk family: the captcha verdict the smoke hit on
		// decimal-id detail calls, and the version-gate message the
		// shape switch exists for. One error, one UI line, no retry.
		res.Err = fmt.Errorf("%s: %w (status=%q msg=%q)", name, ErrTryLater, env.Status, env.Msg)
	default:
		res.Err = fmt.Errorf("%s: %s refused: %q", name, env.Status, env.Msg)
	}
	return res
}

// wait is the politeness gap: sleep out the remainder of minGap since
// this transport's previous request, cancellable by ctx. The first
// request on a fresh transport rides immediately (a transport is per
// refresh — the gap governs the multi-request walks, chiefly the
// detail layers of ticket 03).
func (t *Transport) wait(ctx context.Context) error {
	if t.minGap <= 0 {
		return nil
	}
	t.mu.Lock()
	last := t.lastReq
	t.mu.Unlock()
	if last.IsZero() {
		return nil // the first request on a fresh transport rides now
	}
	remaining := t.minGap - time.Since(last)
	if remaining <= 0 {
		return nil
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isVersionGate matches the version-threshold refusal family the
// shape switch answers ("版本过低" and its spellings). The smoke
// never tripped one, so the match is deliberately broad on the one
// token every observed spelling shares; a false positive still lands
// on the right ladder rung (try later → flip shape).
func isVersionGate(msg string) bool {
	return strings.Contains(msg, "版本")
}
