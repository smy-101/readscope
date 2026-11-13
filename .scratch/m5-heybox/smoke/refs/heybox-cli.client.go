package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.xiaoheihe.cn"
	generalPath    = "/bbs/app/api/general/search/v1"
	topicPath      = "/bbs/app/api/search/topic"
	gamePath       = "/game/search/"
	qrCreatePath   = "/account/get_qrcode_url/"
	qrStatePath    = "/account/qr_state/"
	postTreePath   = "/bbs/app/link/tree"
	repliesPath    = "/bbs/app/comment/sub/comments"
)

type ErrorKind string

const (
	ErrorAuth         ErrorKind = "auth"
	ErrorCaptcha      ErrorKind = "captcha"
	ErrorRateLimit    ErrorKind = "rate_limit"
	ErrorNetwork      ErrorKind = "network"
	ErrorUpstream     ErrorKind = "upstream"
	ErrorIncompatible ErrorKind = "incompatible"
)

type Error struct {
	Kind       ErrorKind
	Message    string
	StatusCode int
	Cause      error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Kind)
}

func (e *Error) Unwrap() error { return e.Cause }

type Query struct {
	Keyword string
	Type    string
	Sort    string
	Offset  int
	Limit   int
}

type ThreadQuery struct {
	LinkID string
	Offset int
	Limit  int
	Sort   string
}

type RepliesQuery struct {
	LinkID        string
	RootCommentID string
	Cursor        string
	Limit         int
}

type Response struct {
	Status string          `json:"status"`
	Msg    string          `json:"msg"`
	Result json.RawMessage `json:"result"`
}

type QRLoginChallenge struct {
	URL       string
	Token     string
	ExpiresIn time.Duration
}

type QRLoginState string

const (
	QRLoginWaiting   QRLoginState = "waiting"
	QRLoginScanned   QRLoginState = "scanned"
	QRLoginSucceeded QRLoginState = "succeeded"
	QRLoginExpired   QRLoginState = "expired"
)

type QRLoginResult struct {
	State        QRLoginState
	Message      string
	HeyboxID     string
	PKey         string
	ExpireAt     string
	XXHHHeyboxID string
}

type stringValue string

func (value *stringValue) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = stringValue(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*value = stringValue(number.String())
		return nil
	}
	if string(data) == "null" {
		*value = ""
		return nil
	}
	return fmt.Errorf("expected string or number")
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	cookie     string
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

type ClientOption func(*Client)

func WithBaseURL(baseURL string) ClientOption {
	return func(client *Client) { client.baseURL = strings.TrimRight(baseURL, "/") }
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(client *Client) { client.httpClient = httpClient }
}

func WithClock(clock func() time.Time) ClientOption {
	return func(client *Client) { client.now = clock }
}

func NewClient(cookie string, timeout time.Duration, options ...ClientOption) *Client {
	jar, _ := cookiejar.New(nil)
	client := &Client{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Jar:     jar,
		},
		cookie: cookie,
		now:    time.Now,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *Client) SearchGeneral(ctx context.Context, query Query) (json.RawMessage, error) {
	params := url.Values{}
	params.Set("keyword", query.Keyword)
	params.Set("q", query.Keyword) // compatibility with older deployments
	params.Set("search_type", generalType(query.Type))
	params.Set("tab_type", generalType(query.Type))
	params.Set("offset", strconv.Itoa(query.Offset))
	params.Set("limit", strconv.Itoa(query.Limit))
	if query.Sort != "" && query.Sort != "relevance" {
		params.Set("sort_filter", query.Sort)
	}
	return c.get(ctx, generalPath, params, true)
}

func (c *Client) SearchTopics(ctx context.Context, query Query) (json.RawMessage, error) {
	params := url.Values{}
	params.Set("q", query.Keyword)
	params.Set("offset", strconv.Itoa(query.Offset))
	params.Set("limit", strconv.Itoa(query.Limit))
	return c.get(ctx, topicPath, params, false)
}

func (c *Client) SearchGames(ctx context.Context, query Query) (json.RawMessage, error) {
	params := url.Values{}
	params.Set("q", query.Keyword)
	params.Set("offset", strconv.Itoa(query.Offset))
	params.Set("limit", strconv.Itoa(query.Limit))
	return c.get(ctx, gamePath, params, false)
}

func (c *Client) GetPostThread(ctx context.Context, query ThreadQuery) (json.RawMessage, error) {
	params := url.Values{}
	params.Set("link_id", query.LinkID)
	params.Set("offset", strconv.Itoa(query.Offset))
	params.Set("limit", strconv.Itoa(query.Limit))
	params.Set("sort_filter", query.Sort)
	return c.get(ctx, postTreePath, params, true)
}

func (c *Client) GetCommentReplies(ctx context.Context, query RepliesQuery) (json.RawMessage, error) {
	params := url.Values{}
	params.Set("link_id", query.LinkID)
	params.Set("root_comment_id", query.RootCommentID)
	params.Set("limit", strconv.Itoa(query.Limit))
	if query.Cursor != "" && query.Cursor != "0" {
		params.Set("lastval", query.Cursor)
	}
	return c.get(ctx, repliesPath, params, true)
}

func (c *Client) CreateQRLogin(ctx context.Context) (QRLoginChallenge, error) {
	params := url.Values{"app": {"web"}}
	envelope, err := c.getEnvelope(ctx, qrCreatePath, params, true)
	if err != nil {
		return QRLoginChallenge{}, err
	}
	if envelope.Status != "ok" {
		return QRLoginChallenge{}, responseError(envelope)
	}
	var result struct {
		QRURL  string          `json:"qr_url"`
		Expire json.RawMessage `json:"expire"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return QRLoginChallenge{}, &Error{Kind: ErrorIncompatible, Message: "小黑盒二维码响应格式已变化", Cause: err}
	}
	result.QRURL = strings.TrimSpace(result.QRURL)
	parsed, err := url.Parse(result.QRURL)
	if err != nil || parsed.Query().Get("qr") == "" {
		return QRLoginChallenge{}, &Error{Kind: ErrorIncompatible, Message: "小黑盒二维码响应缺少 qr 参数", Cause: err}
	}
	return QRLoginChallenge{
		URL:       result.QRURL,
		Token:     parsed.Query().Get("qr"),
		ExpiresIn: parseQRExpiry(result.Expire, c.now()),
	}, nil
}

func (c *Client) PollQRLogin(ctx context.Context, token string) (QRLoginResult, error) {
	params := url.Values{"app": {"web"}, "qr": {token}}
	envelope, err := c.getEnvelope(ctx, qrStatePath, params, true)
	if err != nil {
		return QRLoginResult{}, err
	}
	if envelope.Status == "need_google_check" {
		return QRLoginResult{}, &Error{Kind: ErrorAuth, Message: "该账号需要谷歌二次验证；请改用 heybox login --browser"}
	}
	if envelope.Status != "ok" {
		return QRLoginResult{}, responseError(envelope)
	}
	var result struct {
		Error        string      `json:"error"`
		ErrorMessage string      `json:"error_msg"`
		HeyboxID     stringValue `json:"heybox_id"`
		PKey         stringValue `json:"pkey"`
		ExpireAt     stringValue `json:"expire_at"`
		XXHHHeyboxID stringValue `json:"x_xhh_heyboxid"`
		Profile      struct {
			HeyboxID stringValue `json:"heybox_id"`
		} `json:"profile"`
		AccountDetail struct {
			UserID stringValue `json:"userid"`
		} `json:"account_detail"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return QRLoginResult{}, &Error{Kind: ErrorIncompatible, Message: "小黑盒二维码状态格式已变化", Cause: err}
	}
	switch result.Error {
	case "wait":
		return QRLoginResult{State: QRLoginWaiting}, nil
	case "ready":
		return QRLoginResult{State: QRLoginScanned}, nil
	case "ok":
		heyboxID := firstNonEmpty(
			string(result.Profile.HeyboxID),
			string(result.AccountDetail.UserID),
			string(result.HeyboxID),
			c.cookieValue("heybox_id", "user_heybox_id", "x_xhh_heyboxid"),
		)
		pkey := firstNonEmpty(string(result.PKey), c.cookieValue("pkey", "user_pkey"))
		if strings.TrimSpace(heyboxID) == "" || strings.TrimSpace(pkey) == "" {
			return QRLoginResult{}, &Error{Kind: ErrorIncompatible, Message: "小黑盒二维码登录成功响应缺少 heybox_id 或 pkey"}
		}
		return QRLoginResult{
			State:        QRLoginSucceeded,
			HeyboxID:     heyboxID,
			PKey:         pkey,
			ExpireAt:     string(result.ExpireAt),
			XXHHHeyboxID: firstNonEmpty(string(result.XXHHHeyboxID), c.cookieValue("x_xhh_heyboxid")),
		}, nil
	default:
		message := strings.TrimSpace(result.ErrorMessage)
		if message == "" {
			message = strings.TrimSpace(envelope.Msg)
		}
		if message == "" {
			message = "二维码已失效"
		}
		return QRLoginResult{State: QRLoginExpired, Message: message}, nil
	}
}

func generalType(resultType string) string {
	switch resultType {
	case "post", "all":
		return "link"
	case "topic", "user", "game":
		return resultType
	default:
		return "all"
	}
}

func (c *Client) get(ctx context.Context, path string, params url.Values, signed bool) (json.RawMessage, error) {
	envelope, err := c.getEnvelope(ctx, path, params, signed)
	if err != nil {
		return nil, err
	}
	if envelope.Status != "ok" {
		return nil, responseError(envelope)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return json.RawMessage(`{}`), nil
	}
	return envelope.Result, nil
}

func (c *Client) getEnvelope(ctx context.Context, path string, params url.Values, signed bool) (Response, error) {
	if signed {
		params.Set("os_type", "web")
		params.Set("version", "999.0.3")
		params.Set("x_app", "heybox_website")
		params.Set("x_client_type", "web")
		params.Set("x_os_type", webOSType())

		hkey, nonce, timestamp, err := newSignature(path, c.now())
		if err != nil {
			return Response{}, &Error{Kind: ErrorUpstream, Message: "生成小黑盒请求签名失败", Cause: err}
		}
		params.Set("hkey", hkey)
		params.Set("_time", strconv.FormatInt(timestamp, 10))
		params.Set("nonce", nonce)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(250*(1<<(attempt-1))+rand.IntN(150)) * time.Millisecond
			if err := c.sleep(ctx, delay); err != nil {
				return Response{}, &Error{Kind: ErrorNetwork, Message: "请求已取消", Cause: err}
			}
		}

		result, retry, err := c.requestEnvelope(ctx, path, params)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retry {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

func webOSType() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac"
	case "android":
		return "Android"
	default:
		// The website reports every non-Apple desktop browser as Windows.
		return "Windows"
	}
}

func (c *Client) requestEnvelope(ctx context.Context, path string, params url.Values) (Response, bool, error) {
	requestURL := c.baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return Response{}, false, &Error{Kind: ErrorUpstream, Message: "构造小黑盒请求失败", Cause: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "heybox-cli (+https://www.xiaoheihe.cn)")
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, true, &Error{Kind: ErrorNetwork, Message: "连接小黑盒失败", Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Response{}, true, &Error{Kind: ErrorNetwork, Message: "读取小黑盒响应失败", Cause: err}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Response{}, false, &Error{Kind: ErrorAuth, StatusCode: resp.StatusCode, Message: "小黑盒拒绝了请求；请运行 heybox login 或设置 HEYBOX_COOKIE"}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Response{}, false, &Error{Kind: ErrorRateLimit, StatusCode: resp.StatusCode, Message: "小黑盒请求过于频繁，请稍后再试"}
	}
	if resp.StatusCode >= 500 {
		return Response{}, true, &Error{Kind: ErrorUpstream, StatusCode: resp.StatusCode, Message: fmt.Sprintf("小黑盒服务暂时不可用（HTTP %d）", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, false, &Error{Kind: ErrorUpstream, StatusCode: resp.StatusCode, Message: fmt.Sprintf("小黑盒返回异常状态（HTTP %d）", resp.StatusCode)}
	}

	var envelope Response
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Response{}, false, &Error{Kind: ErrorIncompatible, Message: "小黑盒响应格式已变化", Cause: err}
	}
	return envelope, false, nil
}

func responseError(envelope Response) error {
	switch envelope.Status {
	case "show_captcha", "need_captcha":
		return &Error{Kind: ErrorCaptcha, Message: "小黑盒要求完成验证码；请运行 heybox login 获取登录态后重试"}
	case "lack_token", "need_login", "unauthorized":
		return &Error{Kind: ErrorAuth, Message: "搜索需要小黑盒登录态；请运行 heybox login 或设置 HEYBOX_COOKIE"}
	case "need_google_check":
		return &Error{Kind: ErrorAuth, Message: "该账号需要谷歌二次验证；请改用 heybox login --browser"}
	case "failed":
		message := strings.TrimSpace(envelope.Msg)
		if message == "" {
			message = "小黑盒搜索请求失败"
		}
		if strings.Contains(message, "频繁") || strings.Contains(message, "限流") {
			return &Error{Kind: ErrorRateLimit, Message: message}
		}
		return &Error{Kind: ErrorUpstream, Message: message}
	default:
		return &Error{Kind: ErrorIncompatible, Message: fmt.Sprintf("无法识别的小黑盒响应状态：%s", envelope.Status)}
	}
}

func parseQRExpiry(raw json.RawMessage, now time.Time) time.Duration {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if value == "" || value == "null" {
		return 2 * time.Minute
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil {
		switch {
		case number > 1_000_000_000_000:
			return max(time.UnixMilli(number).Sub(now), time.Second)
		case number > 1_000_000_000:
			return max(time.Unix(number, 0).Sub(now), time.Second)
		case number > 0:
			return time.Duration(number) * time.Second
		}
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return max(parsed.Sub(now), time.Second)
	}
	return 2 * time.Minute
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Client) cookieValue(names ...string) string {
	if c.httpClient == nil || c.httpClient.Jar == nil {
		return ""
	}
	requestURL, err := url.Parse(c.baseURL + qrStatePath)
	if err != nil {
		return ""
	}
	cookies := c.httpClient.Jar.Cookies(requestURL)
	for _, name := range names {
		for _, cookie := range cookies {
			if cookie.Name == name && strings.TrimSpace(cookie.Value) != "" {
				return strings.TrimSpace(cookie.Value)
			}
		}
	}
	return ""
}

func Kind(err error) ErrorKind {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Kind
	}
	return ErrorUpstream
}
