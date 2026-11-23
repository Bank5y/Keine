package eval

import (
	"bytes"
	"fmt"
	"html/template"
	"math/rand"
	"slices"
	"strings"
	"time"
)

// Report is one keine-eval run: every case run Runs times on one prompt
// version and model.
type Report struct {
	Prompt  string    `json:"prompt"`
	Model   string    `json:"model"`
	Started time.Time `json:"started"`
	Runs    int       `json:"runs"`
	Cases   []Case    `json:"cases"`
	Results []Result  `json:"results"`
}

// HardFailures counts the runs that broke a hard rule; there must be none.
func (r Report) HardFailures() int {
	n := 0
	for _, res := range r.Results {
		if res.Hard() {
			n++
		}
	}
	return n
}

func (r Report) resultsOf(id string) []Result {
	var out []Result
	for _, res := range r.Results {
		if res.Case == id {
			out = append(out, res)
		}
	}
	return out
}

// Baseline is what the repository keeps of a run (eval/baseline.json):
// the rules each run broke and its figures, never what the model wrote.
type Baseline struct {
	Prompt       string         `json:"prompt"`
	Model        string         `json:"model"`
	Runs         int            `json:"runs"`
	HardFailures int            `json:"hardFailures"`
	Totals       Totals         `json:"totals"`
	Cases        []BaselineCase `json:"cases"`
}

type BaselineCase struct {
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Kind     string        `json:"kind"`
	Runs     []BaselineRun `json:"runs"`
}

// BaselineRun is a run by its figures: the rules it broke, by name only
// (their details may quote the answer).
type BaselineRun struct {
	Error  string   `json:"error,omitempty"`
	Broken []string `json:"broken"`
	Stats  Stats    `json:"stats"`
}

// Totals are the figures of every run together: averages per run, but
// Rereads and Remedies are how many there were in all.
type Totals struct {
	Steps       float64 `json:"steps"`
	ToolCalls   float64 `json:"toolCalls"`
	Rereads     int     `json:"rereads"`
	Remedies    int     `json:"remedies"`
	DurationMs  float64 `json:"durationMs"`
	FirstTextMs float64 `json:"firstTextMs"`
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	Credits     float64 `json:"credits"`
}

func (r Report) totals() Totals {
	var t Totals
	n, streamed := float64(len(r.Results)), 0.0
	if n == 0 {
		return t
	}
	for _, res := range r.Results {
		s := res.Stats
		t.Steps += float64(s.Steps)
		t.ToolCalls += float64(s.ToolCalls)
		if s.Reread {
			t.Rereads++
		}
		t.Remedies += s.Remedies
		t.DurationMs += float64(s.DurationMs)
		if s.FirstTextMs >= 0 {
			t.FirstTextMs += float64(s.FirstTextMs)
			streamed++
		}
		if s.Usage.Input != nil {
			t.Input += float64(*s.Usage.Input)
		}
		if s.Usage.Output != nil {
			t.Output += float64(*s.Usage.Output)
		}
		t.Credits += s.Credits
	}
	t.Steps, t.ToolCalls, t.DurationMs, t.Input, t.Output, t.Credits = t.Steps/n, t.ToolCalls/n, t.DurationMs/n, t.Input/n, t.Output/n, t.Credits/n
	if streamed > 0 {
		t.FirstTextMs /= streamed
	}
	return t
}

func (r Report) Baseline() Baseline {
	b := Baseline{Prompt: r.Prompt, Model: r.Model, Runs: r.Runs, HardFailures: r.HardFailures(), Totals: r.totals(), Cases: []BaselineCase{}}
	for _, c := range r.Cases {
		bc := BaselineCase{ID: c.ID, Category: c.Category, Kind: c.Kind, Runs: []BaselineRun{}}
		for _, res := range r.resultsOf(c.ID) {
			broken := []string{}
			for _, f := range res.Failures {
				broken = append(broken, f.Rule)
			}
			slices.Sort(broken)
			bc.Runs = append(bc.Runs, BaselineRun{Error: res.Error, Broken: slices.Compact(broken), Stats: res.Stats})
		}
		b.Cases = append(b.Cases, bc)
	}
	return b
}

// Markdown is the run's report for the developer: hard failures first,
// then each case's figures.
func (r Report) Markdown() string {
	var b strings.Builder
	t := r.totals()
	fmt.Fprintf(&b, "# keine-eval %s\n\n", r.Started.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "- 提示词：%s\n- 模型：%s\n- 用例：%d 条，每条 %d 次\n- 硬失败：%d\n\n", r.Prompt, r.Model, len(r.Cases), r.Runs, r.HardFailures())
	fmt.Fprintf(&b, "平均每次：步数 %.1f · 工具 %.1f 次 · 耗时 %.1f s · 首字 %.1f s · 输入 %.0f · 输出 %.0f token · 积分 %.4f；重读本讲 %d 次，补救 %d 次\n\n",
		t.Steps, t.ToolCalls, t.DurationMs/1000, t.FirstTextMs/1000, t.Input, t.Output, t.Credits, t.Rereads, t.Remedies)
	b.WriteString("| 用例 | 类别 | 次 | 结果 | 步数 | 工具 | 重读 | 补救 | 耗时 s | 首字 | 输入 | 输出 | 积分 |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range r.Cases {
		for _, res := range r.resultsOf(c.ID) {
			mark, verdict := "✅", "通过"
			if res.Hard() {
				mark, verdict = "❌", "硬失败"
			}
			s := res.Stats
			fmt.Fprintf(&b, "| %s %s | %s | %d | %s | %d | %d | %s | %d | %.1f | %s | %s | %s | %.4f |\n",
				mark, c.ID, c.Category, res.Attempt, verdict, s.Steps, s.ToolCalls, yes(s.Reread), s.Remedies,
				float64(s.DurationMs)/1000, firstText(s.FirstTextMs), count(s.Usage.Input), count(s.Usage.Output), s.Credits)
		}
	}
	if r.HardFailures() > 0 {
		b.WriteString("\n## 硬失败\n\n")
		for _, res := range r.Results {
			for _, f := range res.Failures {
				fmt.Fprintf(&b, "- ❌ %s 第 %d 次 · `%s`：%s\n", res.Case, res.Attempt, f.Rule, f.Detail)
			}
		}
	}
	return b.String()
}

func yes(b bool) string {
	if b {
		return "是"
	}
	return ""
}

// firstText is a time in seconds, "—" for none (nothing streamed).
func firstText(ms int64) string {
	if ms < 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f s", float64(ms)/1000)
}

func count(p *int64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprint(*p)
}

// side is one version's answer as the blind page shows it.
type side struct {
	Version  string
	Title    string
	Concept  string
	Body     string
	Error    string
	Failures []Failure
	Stats    Stats
}

type pair struct {
	Case    Case
	Attempt int
	Sides   []side
}

// BlindHTML is the page to judge the run against an older one by: each
// case's runs side by side with the old version's, in an order shuffled
// by seed, the versions named only once the reader reveals them. Without
// an older run, the page shows this one alone.
func BlindHTML(cur Report, old *Report, seed int64) []byte {
	rnd := rand.New(rand.NewSource(seed))
	var pairs []pair
	for _, c := range cur.Cases {
		for _, res := range cur.resultsOf(c.ID) {
			p := pair{Case: c, Attempt: res.Attempt, Sides: []side{sideOf(cur.label(), res)}}
			if old != nil {
				for _, o := range old.resultsOf(c.ID) {
					if o.Attempt == res.Attempt {
						p.Sides = append(p.Sides, sideOf(old.label(), o))
					}
				}
			}
			rnd.Shuffle(len(p.Sides), func(i, j int) { p.Sides[i], p.Sides[j] = p.Sides[j], p.Sides[i] })
			pairs = append(pairs, p)
		}
	}
	var buf bytes.Buffer
	if err := blindPage.Execute(&buf, map[string]any{"Pairs": pairs, "Started": cur.Started.Format("2006-01-02 15:04")}); err != nil {
		panic(err) // the template is fixed; it cannot fail on these values
	}
	return buf.Bytes()
}

// label names the run on the blind page once revealed: its prompt, its
// model and when it ran, which tells two runs apart when only one changed.
func (r Report) label() string {
	return r.Prompt + " · " + r.Model + " · " + r.Started.Format("01-02 15:04")
}

func sideOf(version string, r Result) side {
	return side{Version: version, Title: r.Analysis.Title, Concept: r.Analysis.Concept, Body: r.Analysis.Explanation, Error: r.Error, Failures: r.Failures, Stats: r.Stats}
}

var blindPage = template.Must(template.New("blind").Funcs(template.FuncMap{
	"letter": func(i int) string { return string(rune('A' + i)) },
	"secs":   firstText,
}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>keine-eval 盲评</title>
<style>
:root { --bg: #f6f2ec; --surface: #fffdf9; --text: #2a2622; --muted: #7a7068; --line: #e2d9cd; --brand: #b23a2c; }
@media (prefers-color-scheme: dark) { :root { --bg: #1d1b19; --surface: #262320; --text: #ece6dd; --muted: #a39a90; --line: #3a3530; --brand: #e0735f; } }
body { margin: 0; background: var(--bg); color: var(--text); font: 15px/1.7 system-ui, "PingFang SC", "Noto Sans CJK SC", sans-serif; }
header { position: sticky; top: 0; z-index: 1; display: flex; gap: 16px; align-items: center; padding: 12px 24px; background: var(--surface); border-bottom: 1px solid var(--line); }
header h1 { margin: 0; font-size: 17px; }
header p { margin: 0; color: var(--muted); font-size: 13px; flex: 1; }
button { font: inherit; padding: 6px 14px; border: 1px solid var(--brand); background: var(--brand); color: #fff; border-radius: 4px; cursor: pointer; }
main { max-width: 1400px; margin: 0 auto; padding: 16px 24px 48px; }
section { margin: 24px 0; }
section h2 { font-size: 15px; margin: 0 0 8px; }
section h2 small { color: var(--muted); font-weight: normal; }
.quote { color: var(--muted); font-size: 13px; margin: 0 0 10px; }
.sides { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 16px; }
article { background: var(--surface); border: 1px solid var(--line); border-radius: 6px; padding: 14px 16px; min-width: 0; }
article h3 { margin: 0 0 4px; font-size: 14px; color: var(--muted); }
article h4 { margin: 4px 0; font-size: 17px; }
.concept { font-weight: 500; }
.body { white-space: pre-wrap; overflow-wrap: anywhere; }
.error { color: var(--brand); }
.version { display: none; }
.detail { display: none; color: var(--muted); font-size: 12px; border-top: 1px dashed var(--line); margin-top: 10px; padding-top: 8px; }
body.revealed .version { display: inline; color: var(--brand); }
body.revealed .detail { display: block; }
@media (max-width: 600px) { header, main { padding-left: 16px; padding-right: 16px; } }
</style>
</head>
<body>
<header>
  <h1>keine-eval 盲评</h1>
  <p>{{.Started}} · 先读内容，再揭示版本。每组的左右顺序已打乱。</p>
  <button type="button" id="reveal" aria-pressed="false">揭示版本</button>
</header>
<main>
{{range .Pairs}}
<section>
  <h2>{{.Case.ID}} <small>{{.Case.Category}} · {{.Case.Kind}} · 第 {{.Attempt}} 次</small></h2>
  <p class="quote">选区：{{.Case.Quote}}</p>
  <div class="sides">
  {{range $i, $s := .Sides}}
    <article>
      <h3>版本 {{letter $i}} <span class="version">{{$s.Version}}</span></h3>
      {{if $s.Error}}<p class="error">运行出错：{{$s.Error}}</p>{{end}}
      <h4>{{$s.Title}}</h4>
      <p class="concept">{{$s.Concept}}</p>
      <div class="body">{{$s.Body}}</div>
      <div class="detail">
        {{range $s.Failures}}<div class="error">硬失败 {{.Rule}}：{{.Detail}}</div>{{end}}
        步数 {{$s.Stats.Steps}} · 工具 {{$s.Stats.ToolCalls}} 次 · 补救 {{$s.Stats.Remedies}} · 耗时 {{secs $s.Stats.DurationMs}} · 首字 {{secs $s.Stats.FirstTextMs}}
      </div>
    </article>
  {{end}}
  </div>
</section>
{{end}}
</main>
<script>
document.getElementById('reveal').addEventListener('click', (e) => {
  const on = document.body.classList.toggle('revealed');
  e.currentTarget.textContent = on ? '隐藏版本' : '揭示版本';
  e.currentTarget.setAttribute('aria-pressed', String(on));
});
</script>
</body>
</html>
`))
