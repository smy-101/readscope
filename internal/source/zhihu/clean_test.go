package zhihu

// CleanHTML tests — the spec's 内容前处理 rules (spec: Implementation
// Decisions), one test per rule plus the golden that pins a daily
// story's HTML subset through the cleaning AND the M1 pipeline:
// DOM → CleanHTML → ParseHTML → Layout, compared as plain text.
// The render pipeline itself is unchanged by design — cleaning is the
// zhihu package's own front step.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/render"
)

var update = flag.Bool("update", false, "rewrite golden files")

// dailyStoryBodyHTML is the daily-story subset the ticket's golden
// rides on: lazy imgs whose src is a data: placeholder (the real URL
// in data-original, thumbnail-proxied), a noscript duplicate, plain
// paragraphs, a quote.
const dailyStoryBodyHTML = `<div class="main-wrap content-wrap">
<h1>机器知道它在读什么吗</h1>
<p>导语:终端阅读器的时代又回来了。</p>
<p>第一段正文。<img src="data:image/svg+xml;utf8,&lt;svg&gt;" data-original="https://pic1.zhimg.com/50/v2-story_hd.jpg" alt="编辑部配图"></p>
<figure><img src="https://pic2.zhimg.com/80/v2-figure_hd.jpg" data-original="https://pic2.zhimg.com/v2-figure_hd.jpg" alt="题图"></figure>
<noscript><img src="https://pic1.zhimg.com/v2-story_hd.jpg"></noscript>
<blockquote>书读百遍,其义自见。</blockquote>
<p><strong>结语。</strong>「完」</p>
</div>`

func TestCleanStripsNoscriptCopies(t *testing.T) {
	got := CleanHTML(`<p>正文</p><noscript><img src="https://pic1.zhimg.com/a.jpg"></noscript>`)
	if strings.Contains(got, "noscript") {
		t.Fatalf("noscript copy survived:\n%s", got)
	}
	// The noscript img is the same picture the real img above it
	// carries — but even a lone noscript img disappears with its
	// wrapper; the pipeline's own noscript drop stays a defense in
	// depth.
	if strings.Contains(got, "a.jpg") {
		t.Fatalf("noscript's img leaked:\n%s", got)
	}
}

func TestCleanImageSrcPriority(t *testing.T) {
	cases := []struct {
		name, html, want string
	}{
		{"data-original wins", `<img src="https://pic1.zhimg.com/low.jpg" data-original="https://pic1.zhimg.com/orig.jpg">`, "orig.jpg"},
		{"data-actualsrc next", `<img src="https://pic1.zhimg.com/low.jpg" data-actualsrc="https://pic1.zhimg.com/actual.jpg">`, "actual.jpg"},
		{"src last", `<img src="https://pic1.zhimg.com/plain.jpg">`, "plain.jpg"},
	}
	for _, c := range cases {
		if got := CleanHTML(c.html); !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q in\n%s", c.name, c.want, got)
		}
	}
}

func TestCleanStripsZhimgThumbProxy(t *testing.T) {
	for _, proxied := range []string{"/50/", "/80/"} {
		url := "https://pic2.zhimg.com" + proxied + "v2-abc_hd.jpg"
		got := CleanHTML(`<img src="` + url + `">`)
		if strings.Contains(got, proxied) {
			t.Errorf("thumbnail proxy %s survived:\n%s", proxied, got)
		}
		if !strings.Contains(got, "https://pic2.zhimg.com/v2-abc_hd.jpg") {
			t.Errorf("full-size URL not restored:\n%s", got)
		}
	}
	// Only zhimg's size segments go; an unrelated /50/ path elsewhere
	// is none of the cleaner's business.
	got := CleanHTML(`<a href="https://example.com/50/x">stay</a>`)
	if !strings.Contains(got, "/50/x") {
		t.Errorf("non-zhimg path must not be rewritten:\n%s", got)
	}
}

func TestCleanSkipsDataPlaceholders(t *testing.T) {
	got := CleanHTML(`<p>前<img src="data:image/gif;base64,R0lGOD" alt="占位">后</p>`)
	if strings.Contains(got, "<img") {
		t.Fatalf("data: placeholder img must be dropped entirely:\n%s", got)
	}
	if !strings.Contains(got, "前") || !strings.Contains(got, "后") {
		t.Fatalf("surrounding text must survive:\n%s", got)
	}
}

func TestCleanStickerBecomesAltText(t *testing.T) {
	got := CleanHTML(`<p>哈哈<img src="https://pic4.zhimg.com/v2-sticker.png" data-sticker-id="99" alt="[捂脸]">结尾</p>`)
	if strings.Contains(got, "<img") || strings.Contains(got, "sticker") {
		t.Fatalf("sticker must not render as an image link:\n%s", got)
	}
	if !strings.Contains(got, "[捂脸]") {
		t.Fatalf("sticker alt text must survive in place:\n%s", got)
	}
	// A sticker without alt text just goes.
	if got := CleanHTML(`<img data-sticker-id="1" src="https://pic4.zhimg.com/s.png">`); strings.Contains(got, "img") {
		t.Fatalf("alt-less sticker should be dropped:\n%s", got)
	}
}

func TestCleanVideoBoxBecomesVideoLink(t *testing.T) {
	got := CleanHTML(`<a class="video-box" href="https://www.zhihu.com/video/1234" data-lens-id="1234"><span class="box">thumb</span></a>`)
	if !strings.Contains(got, `href="https://www.zhihu.com/video/1234"`) {
		t.Fatalf("video link must keep its page URL:\n%s", got)
	}
	if !strings.Contains(got, "[视频]") || strings.Contains(got, "video-box") {
		t.Fatalf("video card should become a bare [视频] link:\n%s", got)
	}
	// No href aboard: build the lens page from data-lens-id.
	got = CleanHTML(`<a class="video-box" data-lens-id="77"></a>`)
	if !strings.Contains(got, `href="https://www.zhihu.com/lens/77"`) {
		t.Fatalf("lens fallback missing:\n%s", got)
	}
}

func TestCleanMergesAdjacentSameImages(t *testing.T) {
	got := CleanHTML(`<p><img src="https://pic1.zhimg.com/a.jpg"><img src="https://pic1.zhimg.com/a.jpg"></p>`)
	if n := strings.Count(got, "a.jpg"); n != 1 {
		t.Fatalf("duplicate adjacent image should merge into one, found %d:\n%s", n, got)
	}
	// Different neighbours keep their own imgs.
	got = CleanHTML(`<p><img src="https://pic1.zhimg.com/a.jpg"><img src="https://pic1.zhimg.com/b.jpg"></p>`)
	if n := strings.Count(got, "<img"); n != 2 {
		t.Fatalf("different images must both stay, found %d:\n%s", n, got)
	}
}

// The golden pins the daily story subset end to end: the cleaned body
// through the M1 pipeline's plain-text layout at width 60 — 图片降级
// markers with the FULL-size link, no noscript echo, quote bar text.
func TestGoldenDailyStoryThroughPipeline(t *testing.T) {
	doc, err := render.ParseHTML(CleanHTML(dailyStoryBodyHTML))
	if err != nil {
		t.Fatalf("parse cleaned body: %v", err)
	}
	got := render.ToPlain(render.Layout(doc, 60)) + "\n"
	golden := filepath.Join("testdata", "daily-story.golden")
	if *update {
		os.WriteFile(golden, []byte(got), 0o644)
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run go test ./internal/source/zhihu -update first): %v", err)
	}
	if got != string(want) {
		t.Errorf("layout differs from golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
