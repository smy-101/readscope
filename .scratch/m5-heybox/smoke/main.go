// M5 冒烟脚本:验证小黑盒 web 端点 × 签名变体 A/B 的实际可达性。
// 参考:.scratch/m5-heybox/research.md §9 冒烟清单。
// 运行:go run . (仅标准库)
package main

import (
	"crypto/md5" // #nosec G501 -- 上游协议要求
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	baseURL    = "https://api.xiaoheihe.cn"
	uaBrowser  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	referer    = "https://www.xiaoheihe.cn/"
	samplesDir = "samples"
	deviceID   = "a1b2c3d4e5f60718293a4b5c6d7e8f90" // 固定 32hex,模拟 web 端 device_id
)

// ---------- 变体 A:heybox-cli signer.go / RSSHub util.ts(huandu/heybox-url) ----------

const alphabetA = "JKMNPQRTX1234OABCDFG56789H"

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) // #nosec G401 -- 协议兼容,非安全用途
	return hex.EncodeToString(sum[:])
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func xtime(v byte) byte {
	if v&128 != 0 {
		return byte((int(v)<<1 ^ 27) & 255)
	}
	return v << 1
}

func zA(v byte) byte  { return xtime(v) ^ v }
func jA(v byte) byte  { return zA(xtime(v)) }
func aaA(v byte) byte { return jA(zA(xtime(v))) }
func oA(v byte) byte  { return aaA(v) ^ jA(v) ^ zA(v) }

func signA(path string, ts int64, nonce string) string {
	canonical := "/" + strings.Join(strings.FieldsFunc(path, func(r rune) bool { return r == '/' }), "/") + "/"
	seed := md5Hex(onlyDigits(nonce + alphabetA))
	digits := onlyDigits(md5Hex(fmt.Sprintf("%d%s%s", ts+1, canonical, seed)))
	if len(digits) > 9 {
		digits = digits[:9]
	}
	for len(digits) < 9 {
		digits += "0"
	}
	var n int64
	for _, r := range digits {
		n = n*10 + int64(r-'0')
	}
	enc := make([]byte, 0, 5)
	for range 5 {
		enc = append(enc, alphabetA[n%int64(len(alphabetA))])
		n /= int64(len(alphabetA))
	}
	a, b, c, d := enc[1], enc[2], enc[3], enc[4] // 末 4 字节
	mixed := [4]byte{
		oA(a) ^ aaA(b) ^ jA(c) ^ zA(d),
		zA(a) ^ oA(b) ^ aaA(c) ^ jA(d),
		jA(a) ^ zA(b) ^ oA(c) ^ aaA(d),
		aaA(a) ^ jA(b) ^ zA(c) ^ oA(d),
	}
	checksum := (int(mixed[0]) + int(mixed[1]) + int(mixed[2]) + int(mixed[3])) % 100
	return string(enc) + fmt.Sprintf("%02d", checksum)
}

// ---------- 变体 B:WenfuRainbow/heybox signature.ts ----------

const alphabetB = "AB45STUVWZEFGJ6CH01D237IXYPQRKLMN89"

func vm(e byte) byte { return xtime(e) }
func qm(e byte) byte { return vm(e) ^ e }
func dm(e byte) byte { return qm(vm(e)) }
func ym(e byte) byte { return dm(qm(vm(e))) }
func gm(e byte) byte { return ym(e) ^ dm(e) ^ qm(e) }

// av:经字母表前 offset 个字符编码(offset 为负取尾部,JS slice 语义)
func avB(text string, offset int) string {
	alpha := alphabetB
	if offset < 0 {
		alpha = alpha[:len(alpha)+offset]
	}
	var b strings.Builder
	for _, r := range text {
		b.WriteByte(alpha[int(r)%len(alpha)])
	}
	return b.String()
}

func svB(text string) string {
	var b strings.Builder
	for _, r := range text {
		b.WriteByte(alphabetB[int(r)%len(alphabetB)])
	}
	return b.String()
}

func interleave(arr []string) string {
	max := 0
	for _, s := range arr {
		if len(s) > max {
			max = len(s)
		}
	}
	var b strings.Builder
	for i := 0; i < max; i++ {
		for _, s := range arr {
			if i < len(s) {
				b.WriteByte(s[i])
			}
		}
	}
	return b.String()
}

func kmB(e []byte) []byte {
	t := [4]byte{
		gm(e[0]) ^ ym(e[1]) ^ dm(e[2]) ^ qm(e[3]),
		qm(e[0]) ^ gm(e[1]) ^ ym(e[2]) ^ dm(e[3]),
		dm(e[0]) ^ qm(e[1]) ^ gm(e[2]) ^ ym(e[3]),
		ym(e[0]) ^ dm(e[1]) ^ qm(e[2]) ^ gm(e[3]),
	}
	copy(e, t[:])
	return e
}

func signB(path string, ts int64, nonce string) string {
	canonical := "/" + strings.Join(strings.FieldsFunc(path, func(r rune) bool { return r == '/' }), "/") + "/"
	encT := avB(strconv.FormatInt(ts+1, 10), -2)
	encP := svB(canonical)
	encN := svB(nonce)
	interleaved := interleave([]string{encT, encP, encN})
	if len(interleaved) > 20 {
		interleaved = interleaved[:20]
	}
	hash := md5Hex(interleaved)
	last6 := kmB([]byte(hash[len(hash)-6:]))
	sum := 0
	for _, v := range last6 {
		sum += int(v)
	}
	prefix := avB(hash[:5], -4)
	return prefix + fmt.Sprintf("%02d", sum%100)
}

// ---------- 参考向量(node 跑原版 JS 生成,见 refs/gen.mjs) ----------

var refVectors = []struct {
	path  string
	ts    int64
	nonce string
	a, b  string
}{
	{"/bbs/app/feeds/news", 1760000000, "ABCDEF0123456789ABCDEF0123456789", "826NB12", "X03Y761"},
	{"/bbs/app/link/tree", 1760000000, "ABCDEF0123456789ABCDEF0123456789", "KP9HN82", "X03Y761"},
	{"/bbs/app/comment/sub/comments", 1760000000, "ABCDEF0123456789ABCDEF0123456789", "JBAOM94", "X03Y761"},
	{"/bbs/app/topic/feeds", 1760000000, "ABCDEF0123456789ABCDEF0123456789", "PKAJQ64", "X03Y761"},
}

func selfCheck() error {
	for _, v := range refVectors {
		if got := signA(v.path, v.ts, v.nonce); got != v.a {
			return fmt.Errorf("变体A 向量不符 %s: got %s want %s", v.path, got, v.a)
		}
		if got := signB(v.path, v.ts, v.nonce); got != v.b {
			return fmt.Errorf("变体B 向量不符 %s: got %s want %s", v.path, got, v.b)
		}
	}
	return nil
}

// ---------- HTTP ----------

type envelope struct {
	Status string          `json:"status"`
	Msg    string          `json:"msg"`
	Result json.RawMessage `json:"result"`
}

func applySign(params url.Values, path string, variant string, now time.Time) {
	ts := now.Unix()
	nonce := strings.ToUpper(md5Hex(fmt.Sprintf("%d%s", ts, hex.EncodeToString(random16()))))
	var hkey string
	switch variant {
	case "A":
		hkey = signA(path, ts, nonce)
	case "B":
		hkey = signB(path, ts, nonce)
	default:
		panic("unknown variant " + variant)
	}
	params.Set("hkey", hkey)
	params.Set("_time", strconv.FormatInt(ts, 10))
	params.Set("nonce", nonce)
}

func random16() []byte {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return b
}

func getJSON(client *http.Client, step, path string, params url.Values, variant string) (envelope, int, []byte, error) {
	if variant == "A" || variant == "B" {
		applySign(params, path, variant, time.Now())
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return envelope{}, 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", uaBrowser)
	req.Header.Set("Referer", referer)
	resp, err := client.Do(req)
	if err != nil {
		return envelope{}, 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return envelope{}, resp.StatusCode, body, err
	}
	var env envelope
	_ = json.Unmarshal(body, &env)
	name := filepath.Join(samplesDir, step+".json")
	_ = os.MkdirAll(samplesDir, 0o755)
	_ = os.WriteFile(name, body, 0o644)
	verdict := env.Status
	if verdict == "" {
		verdict = "HTTP-" + strconv.Itoa(resp.StatusCode)
	}
	fmt.Printf("%-28s %-3s → HTTP %d  status=%s  msg=%q  %d bytes  (%s)\n",
		step, variant, resp.StatusCode, verdict, env.Msg, len(body), name)
	return env, resp.StatusCode, body, nil
}

// ---------- 通用 JSON 取值(string/number 双容忍) ----------

func dig(root any, path ...string) any {
	cur := root
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func toID(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	}
	return ""
}

func main() {
	if err := selfCheck(); err != nil {
		fmt.Println("自检失败:", err)
		os.Exit(1)
	}
	fmt.Println("签名器自检通过(4 向量 × 2 变体,与原版 JS 输出一致)")

	client := &http.Client{Timeout: 20 * time.Second}
	var lastErr error
	fail := func(step string, err error) {
		fmt.Printf("%-28s 跳过:%v\n", step, err)
		lastErr = err
	}

	// ---- S1/S2:免登录新闻流,变体 A vs B(RSSHub 参数) ----
	newsParams := func() url.Values {
		return url.Values{
			"os_type": {"web"}, "app": {"heybox"}, "client_type": {"mobile"},
			"version": {"999.0.3"}, "x_client_type": {"web"}, "x_os_type": {"Mac"},
			"x_app": {"heybox"}, "heybox_id": {"-1"}, "appid": {"900018355"},
			"offset": {"0"}, "limit": {"20"},
		}
	}
	var linkID string
	if override := os.Getenv("SMOKE_LINK_ID"); override != "" {
		linkID = override
		fmt.Printf("SMOKE_LINK_ID override: link_id = %q\n", linkID)
	} else {
		env1, _, body1, err := getJSON(client, "01-feeds-news-A", "/bbs/app/feeds/news", newsParams(), "A")
		if err != nil {
			fail("01-feeds-news-A", err)
		}
		time.Sleep(1500 * time.Millisecond)
		time.Sleep(1500 * time.Millisecond)
		_, _, _, _ = getJSON(client, "02-feeds-news-B", "/bbs/app/feeds/news", newsParams(), "B")
		// 对照:不签名
		_, _, _, _ = getJSON(client, "03-feeds-news-unsigned", "/bbs/app/feeds/news", newsParams(), "")
		time.Sleep(1500 * time.Millisecond)

		var linkID string
		if env1.Status == "ok" {
			var root any
			if err := json.Unmarshal(body1, &root); err == nil {
				if links, ok := dig(root, "result", "links").([]any); ok {
					for _, it := range links {
						id := toID(dig(it, "linkid"))
						if id != "" { // 跳过广告卡(content_type=23 无 linkid)
							linkID = id
							break
						}
					}
				}
				fmt.Printf("提取 link_id = %q\n", linkID)
			}
		}
		time.Sleep(3 * time.Second) // interval to lower captcha odds
	}

	// ---- S4/S5:帖子树(link/tree,heybox-cli 参数)----
	treeParams := func() url.Values {
		return url.Values{
			"os_type": {"web"}, "app": {"heybox"}, "client_type": {"web"},
			"version": {"999.0.4"}, "x_client_type": {"web"}, "x_app": {"heybox_website"},
			"heybox_id": {"-1"}, "x_os_type": {"Windows"}, "device_info": {"Chrome"},
			"device_id": {deviceID},
			"link_id":   {linkID}, "offset": {"0"}, "limit": {"20"}, "sort_filter": {""},
		}
	}
	var rootCommentID string
	if linkID != "" {
		envA, _, bodyA, err := getJSON(client, "04-link-tree-A", "/bbs/app/link/tree", treeParams(), "A")
		if err != nil {
			fail("04-link-tree-A", err)
		}
		time.Sleep(1500 * time.Millisecond)
		_, _, _, err = getJSON(client, "05-link-tree-B", "/bbs/app/link/tree", treeParams(), "B")
		if err != nil {
			fail("05-link-tree-B", err)
		}
		time.Sleep(1500 * time.Millisecond)
		if envA.Status == "ok" {
			var root any
			if err := json.Unmarshal(bodyA, &root); err == nil {
				if cs, ok := dig(root, "result", "comments").([]any); ok && len(cs) > 0 {
					// CommentGroup 可能是数组(楼层+子回复)或对象(含 comment 字段)
					switch g := cs[0].(type) {
					case []any:
						if len(g) > 0 {
							rootCommentID = toID(dig(g[0], "commentid"))
						}
					case map[string]any:
						if inner, ok := g["comment"].(map[string]any); ok {
							rootCommentID = toID(dig(inner, "commentid"))
						} else {
							rootCommentID = toID(dig(g, "commentid"))
						}
					}
				}
			}
			fmt.Printf("提取 root_comment_id = %q\n", rootCommentID)
		}
	} else {
		fmt.Println("04/05 跳过:无 link_id")
	}

	// ---- S6:免登录详情兜底 share/data(RSSHub 参数)----
	if linkID != "" {
		shareParams := url.Values{
			"os_type": {"web"}, "app": {"heybox"}, "client_type": {"web"},
			"version": {"999.0.4"}, "x_client_type": {"web"}, "x_app": {"heybox_website"},
			"heybox_id": {"-1"}, "x_os_type": {"Windows"}, "device_info": {"Chrome"},
			"device_id": {deviceID},
			"offset":    {"0"}, "limit": {"3"}, "link_id": {linkID}, "use_concept_type": {""},
		}
		if _, _, _, err := getJSON(client, "06-share-data-A", "/bbs/app/api/share/data/", shareParams, "A"); err != nil {
			fail("06-share-data-A", err)
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// ---- S7:话题搜索(heybox-cli:无签名)→ 提取 topic_id ----
	searchParams := url.Values{"q": {"黑神话"}, "offset": {"0"}, "limit": {"5"}}
	var topicID string
	if env7, _, body7, err := getJSON(client, "07-search-topic-unsigned", "/bbs/app/api/search/topic", searchParams, ""); err != nil {
		fail("07-search-topic", err)
	} else if env7.Status == "ok" {
		var root any
		if err := json.Unmarshal(body7, &root); err == nil {
			for _, k := range []string{"topics", "topic_list", "list"} {
				if arr, ok := dig(root, "result", k).([]any); ok && len(arr) > 0 {
					topicID = toID(dig(arr[0], "topic_id"))
					if topicID == "" {
						topicID = toID(dig(arr[0], "id"))
					}
					if topicID != "" {
						break
					}
				}
			}
		}
		fmt.Printf("提取 topic_id = %q\n", topicID)
	}
	time.Sleep(1500 * time.Millisecond)

	// ---- S8/S9:话题流 topic/feeds,A 然后(仅当 A 失败)B ----
	if topicID != "" {
		topicParams := func() url.Values {
			return url.Values{
				"os_type": {"web"}, "version": {"999.0.3"}, "x_app": {"heybox_website"},
				"x_client_type": {"web"}, "x_os_type": {"Windows"},
				"topic_id": {topicID}, "offset": {"0"}, "limit": {"10"}, "lastval": {""}, "dw": {"720"},
			}
		}
		envT, _, _, err := getJSON(client, "08-topic-feeds-A", "/bbs/app/topic/feeds", topicParams(), "A")
		if err != nil {
			fail("08-topic-feeds-A", err)
		}
		time.Sleep(1500 * time.Millisecond)
		if envT.Status != "ok" {
			if _, _, _, err := getJSON(client, "09-topic-feeds-B", "/bbs/app/topic/feeds", topicParams(), "B"); err != nil {
				fail("09-topic-feeds-B", err)
			}
			time.Sleep(1500 * time.Millisecond)
		}

		// ---- S10:楼中楼 sub/comments(heybox-cli 参数,变体 A)----
		if rootCommentID != "" {
			subParams := url.Values{
				"os_type": {"web"}, "version": {"999.0.3"}, "x_app": {"heybox_website"},
				"x_client_type": {"web"}, "x_os_type": {"Windows"},
				"link_id": {linkID}, "root_comment_id": {rootCommentID}, "limit": {"20"},
			}
			if _, _, _, err := getJSON(client, "10-sub-comments-A", "/bbs/app/comment/sub/comments", subParams, "A"); err != nil {
				fail("10-sub-comments-A", err)
			}
			time.Sleep(1500 * time.Millisecond)
		} else {
			fmt.Println("10 跳过:无 root_comment_id")
		}
	} else {
		fmt.Println("08/09/10 跳过:无 topic_id")
	}

	// ---- S11:免签用户动态(RSSHub 生产路由参数)----
	eventsParams := func() url.Values {
		return url.Values{
			"lang": {"zh-cn"}, "version": {"1.3.303"}, "userid": {"30664023"}, "list_type": {"moment"},
		}
	}
	if envE, _, _, err := getJSON(client, "11-profile-events-unsigned", "/bbs/app/profile/events", eventsParams(), ""); err != nil {
		fail("11-profile-events", err)
	} else if envE.Status != "ok" {
		// 重试 11b:web 公参 + 变体 A 签名 + heybox_id=-1
		time.Sleep(1500 * time.Millisecond)
		signed := url.Values{
			"os_type": {"web"}, "version": {"999.0.3"}, "x_app": {"heybox_website"},
			"x_client_type": {"web"}, "x_os_type": {"Windows"}, "heybox_id": {"-1"},
			"userid": {"30664023"}, "list_type": {"moment"},
		}
		if _, _, _, err := getJSON(client, "11b-profile-events-signedA", "/bbs/app/profile/events", signed, "A"); err != nil {
			fail("11b-profile-events", err)
		}
	}

	if lastErr != nil {
		fmt.Println("(部分步骤出错,见上)")
	}
	fmt.Println("完成。samples/ 下为原始响应。")
}
