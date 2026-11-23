// Command keine-eval evaluates the exploration assistant's analysis on real
// courses (#140): after a change to the prompts, the default model, the
// context assembly or the tools, it runs the cases in eval/cases on the
// real loop and checks what comes out. It is not run in CI.
//
// It starts with the deployment's database and file storage settings, the
// same environment as keine (KEINE_DATABASE_URL, KEINE_STORAGE…, and
// KEINE_MASTER_KEY to open the site model's key), and only reads: it pins
// nothing, records no usage and signs in to no instance. Give it a
// read-only database account; it does not migrate the database.
//
//	go run ./cmd/keine-eval                       # every case, twice, on the site's analysis model
//	go run ./cmd/keine-eval -only http-term,quant-sentence -runs 1
//	go run ./cmd/keine-eval -model eval/models/deepseek.yaml
//	go run ./cmd/keine-eval -baseline             # also write eval/baseline.json
//
// Each run writes .scratch/eval/<time>-<prompt version>/: results.json (the
// whole answers), report.md, blind.html (side by side with the run given by
// -against, the latest before by default; reveal the versions after
// reading) and baseline.json (figures and rules only). It exits 1 when any
// run broke a hard rule.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/assistant/adapters"
	"github.com/Bank5y/keine/internal/assistant/search"
	"github.com/Bank5y/keine/internal/config"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/db"
	"github.com/Bank5y/keine/internal/eval"
	"github.com/Bank5y/keine/internal/secret"
)

func main() {
	os.Exit(run())
}

type options struct {
	cases, out, against, model, only, web, mermaid string
	runs, parallel                                 int
	baseline                                       bool
	deadline                                       time.Duration
}

func run() int {
	var o options
	flag.StringVar(&o.cases, "cases", "eval/cases", "the folder of case files")
	flag.StringVar(&o.out, "out", ".scratch/eval", "where each run's folder goes")
	flag.StringVar(&o.against, "against", "latest", "the earlier run to compare with in blind.html: latest, none, or its folder")
	flag.StringVar(&o.model, "model", "", "a model file (YAML) to run on instead of the site's analysis model")
	flag.StringVar(&o.only, "only", "", "run only these cases (ids, comma separated)")
	flag.StringVar(&o.web, "web", "web", "the front end's folder, whose node_modules has mermaid")
	flag.StringVar(&o.mermaid, "mermaid", "eval/mermaid-check.mjs", "the Mermaid parsing script")
	flag.IntVar(&o.runs, "runs", 2, "how many times each case runs")
	flag.IntVar(&o.parallel, "parallel", 4, "how many runs go at once")
	flag.BoolVar(&o.baseline, "baseline", false, "write the run's baseline to eval/baseline.json")
	flag.DurationVar(&o.deadline, "deadline", assistant.DefaultDeadline, "a run's deadline")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep, dir, err := evaluate(ctx, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keine-eval:", err)
		if dir != "" {
			fmt.Printf("\n%s\n", rep.Markdown())
		}
		return 2
	}
	fmt.Printf("\n%s\n%d 次运行，硬失败 %d 次\n%s\n", rep.Markdown(), len(rep.Results), rep.HardFailures(), dir)
	if rep.HardFailures() > 0 {
		return 1
	}
	return 0
}

func evaluate(ctx context.Context, o options) (eval.Report, string, error) {
	cases, err := eval.LoadCases(o.cases)
	if err != nil {
		return eval.Report{}, "", err
	}
	if o.only != "" {
		want := strings.Split(o.only, ",")
		cases = slices.DeleteFunc(cases, func(c eval.Case) bool { return !slices.Contains(want, c.ID) })
		if len(cases) == 0 {
			return eval.Report{}, "", fmt.Errorf("no case named %s", o.only)
		}
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return eval.Report{}, "", err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return eval.Report{}, "", err
	}
	defer pool.Close()
	files, err := cfg.OpenStore(ctx)
	if err != nil {
		return eval.Report{}, "", err
	}
	var box *secret.Box
	if cfg.MasterKey != "" {
		if box, err = secret.NewBox(cfg.MasterKey); err != nil {
			return eval.Report{}, "", err
		}
	}
	turn, err := modelTurn(ctx, o.model, assistant.NewModels(pool, box, adapters.For, search.For))
	if err != nil {
		return eval.Report{}, "", err
	}
	r := &eval.Runner{
		Courses: course.NewService(pool, files), Deadline: o.deadline,
		Mermaid: eval.NodeMermaid{Script: o.mermaid, Web: o.web},
		Turn:    func(context.Context) (assistant.Turn, error) { return turn, nil },
	}
	rep := eval.Report{Prompt: assistant.AnalysisPrompt.Version, Model: turn.DisplayName, Started: time.Now(), Runs: o.runs, Cases: cases}
	// what finished is kept even when some runs could not be made: their
	// answers cost money
	var runErr error
	rep.Results, runErr = runAll(ctx, r, cases, o)
	dir, err := write(rep, o)
	if runErr != nil {
		return rep, dir, fmt.Errorf("some runs could not be made (what finished is in %s): %w", dir, runErr)
	}
	return rep, dir, err
}

// runAll runs every case o.runs times, o.parallel at once, and returns
// the results by case and attempt.
func runAll(ctx context.Context, r *eval.Runner, cases []eval.Case, o options) ([]eval.Result, error) {
	type job struct {
		c       eval.Case
		attempt int
	}
	var jobs []job
	for _, c := range cases {
		for a := 1; a <= o.runs; a++ {
			jobs = append(jobs, job{c, a})
		}
	}
	results := make([]eval.Result, len(jobs))
	done := make([]bool, len(jobs))
	errs := make([]error, len(jobs))
	sem := make(chan struct{}, max(o.parallel, 1))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := r.Run(ctx, j.c, j.attempt)
			results[i], errs[i], done[i] = res, err, err == nil
			mark := "ok  "
			if res.Hard() {
				mark = "FAIL"
			}
			fmt.Fprintf(os.Stderr, "%s %s #%d %.1fs\n", mark, j.c.ID, j.attempt, float64(res.Stats.DurationMs)/1000)
		})
	}
	wg.Wait()
	var finished []eval.Result
	for i, res := range results {
		if done[i] {
			finished = append(finished, res)
		}
	}
	return finished, errors.Join(errs...)
}

// write keeps the run in its folder, and the baseline in the repository
// when asked to.
// baselineFile is the baseline the repository keeps.
const baselineFile = "eval/baseline.json"

func write(rep eval.Report, o options) (string, error) {
	dir := filepath.Join(o.out, rep.Started.Format("20060102-150405")+"-"+rep.Prompt)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	old, err := earlier(o.out, dir, o.against)
	if err != nil {
		return "", err
	}
	baseline, _ := json.MarshalIndent(rep.Baseline(), "", "  ")
	results, _ := json.MarshalIndent(rep, "", "  ")
	for name, data := range map[string][]byte{
		"results.json":  results,
		"report.md":     []byte(rep.Markdown()),
		"blind.html":    eval.BlindHTML(rep, old, rep.Started.UnixNano()),
		"baseline.json": baseline,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return "", err
		}
	}
	if o.baseline {
		if err := os.WriteFile(baselineFile, append(baseline, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// earlier is the run to compare with: the latest folder in out before
// this one, the one named, or none.
func earlier(out, current, against string) (*eval.Report, error) {
	switch against {
	case "none":
		return nil, nil
	case "latest":
		dirs, _ := filepath.Glob(filepath.Join(out, "*", "results.json"))
		slices.Sort(dirs)
		dirs = slices.DeleteFunc(dirs, func(d string) bool { return filepath.Dir(d) == current })
		if len(dirs) == 0 {
			return nil, nil
		}
		against = filepath.Dir(dirs[len(dirs)-1])
	}
	raw, err := os.ReadFile(filepath.Join(against, "results.json"))
	if err != nil {
		return nil, fmt.Errorf("-against: %w", err)
	}
	var r eval.Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("-against %s: %w", against, err)
	}
	return &r, nil
}

// modelFile is a model given with -model: any OpenAI-compatible or
// Anthropic endpoint. Keys come from the environment, never the file.
type modelFile struct {
	Format        string             `yaml:"format"`
	Provider      assistant.Provider `yaml:"provider"`
	BaseURL       string             `yaml:"baseUrl"`
	KeyEnv        string             `yaml:"keyEnv"`
	Model         string             `yaml:"model"`
	DisplayName   string             `yaml:"displayName"`
	ContextWindow int                `yaml:"contextWindow"`
	Thinking      bool               `yaml:"thinking"`
	// Search is off, provider (Anthropic), deepseek, or tavily (with the
	// key in TavilyKeyEnv).
	Search       assistant.SearchRoute `yaml:"search"`
	TavilyKeyEnv string                `yaml:"tavilyKeyEnv"`
	// Rates are credits per million tokens and per search, to work out
	// what a run would cost; all zero leaves it at 0.
	Rates struct {
		Input      float64 `yaml:"input"`
		CacheRead  float64 `yaml:"cacheRead"`
		CacheWrite float64 `yaml:"cacheWrite"`
		Output     float64 `yaml:"output"`
		Search     float64 `yaml:"search"`
	} `yaml:"rates"`
}

// modelTurn is the model the runs take: the site's analysis model, or the
// one in the file.
func modelTurn(ctx context.Context, file string, models *assistant.Models) (assistant.Turn, error) {
	if file == "" {
		t, err := models.ForTurn(ctx, assistant.TurnAnalysis)
		if err != nil {
			return t, fmt.Errorf("the site's analysis model: %w (set KEINE_MASTER_KEY, or give -model)", err)
		}
		return t, nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return assistant.Turn{}, err
	}
	var m modelFile
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return assistant.Turn{}, fmt.Errorf("%s: %w", file, err)
	}
	if m.Model == "" || (m.Format != assistant.FormatOpenAI && m.Format != assistant.FormatAnthropic) {
		return assistant.Turn{}, fmt.Errorf("%s: needs format (openai or anthropic) and model", file)
	}
	if m.BaseURL == "" {
		m.BaseURL = assistant.DefaultBaseURL(m.Format, m.Provider)
	}
	if m.BaseURL == "" {
		return assistant.Turn{}, fmt.Errorf("%s: needs baseUrl", file)
	}
	key := ""
	if m.KeyEnv != "" {
		if key = os.Getenv(m.KeyEnv); key == "" {
			return assistant.Turn{}, fmt.Errorf("%s: %s is not set", file, m.KeyEnv)
		}
	}
	t := assistant.Turn{
		Adapter: adapters.For(assistant.Endpoint{Format: m.Format, Provider: m.Provider, BaseURL: m.BaseURL, Key: key}),
		Model:   m.Model, DisplayName: m.DisplayName, Thinking: m.Thinking, ContextWindow: m.ContextWindow, Rates: assistant.Rates(m.Rates),
		ProviderWeb: m.Search == assistant.RouteProvider,
	}
	if t.DisplayName == "" {
		t.DisplayName = m.Model
	}
	if t.ContextWindow <= 0 {
		t.ContextWindow = assistant.DefaultContextWindow
	}
	switch m.Search {
	case "", assistant.RouteOff, assistant.RouteProvider:
	case assistant.RouteDeepSeek:
		w := search.For(assistant.RouteDeepSeek, key)
		t.Web = &w
	case assistant.RouteTavily:
		tk := os.Getenv(m.TavilyKeyEnv)
		if tk == "" {
			return assistant.Turn{}, fmt.Errorf("%s: Tavily search needs tavilyKeyEnv set", file)
		}
		w := search.For(assistant.RouteTavily, tk)
		t.Web = &w
	default:
		return assistant.Turn{}, fmt.Errorf("%s: search must be off, provider, deepseek or tavily", file)
	}
	return t, nil
}
