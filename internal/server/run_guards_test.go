package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/server"
)

func withMaxRuns(n int) appOption {
	return func(o *server.Options) { o.MaxRuns = n }
}

// gated is a step that waits for open to close before it answers; the
// test closing open is how the run ends.
func gated(open chan struct{}) step {
	s := texts(goodAnswer)
	s.before = func() { <-open }
	return s
}

// running starts an analysis that runs until open closes; wait returns
// once it has ended.
func running(t *testing.T, m *scriptedModel, c *client, courseID, lessonID int) (wait func()) {
	t.Helper()
	before := len(m.calls())
	done := make(chan struct{})
	go func() {
		defer close(done)
		analyze(t, c, courseID, lessonID, agentQuote)
	}()
	for deadline := time.Now().Add(5 * time.Second); len(m.calls()) == before; {
		if time.Now().After(deadline) {
			t.Fatal("the run never reached the model")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() { <-done }
}

// gate is a channel the test closes to let a gated run end, closed at
// the end of the test too, so a failing test leaves no run waiting.
func gate(t *testing.T) (open chan struct{}, release func()) {
	open = make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(open) }) }
	t.Cleanup(release)
	return open, release
}

func TestWhenEveryRunIsTakenTheNextIsTurnedAwayAtOnce(t *testing.T) {
	open, release := gate(t)
	m := newScriptedModel(gated(open), texts(goodAnswer))
	a := aiAppWith(t, m, false, withMaxRuns(1))
	bob, alice := a.user("bob"), a.user("alice")
	id, lesson := analysisColumn(t, bob)

	hers, herLesson := analysisColumn(t, alice)

	wait := running(t, m, bob, id, lesson)
	_, r := analyze(t, alice, hers, herLesson, agentQuote)
	expectError(t, r, http.StatusServiceUnavailable, "busy")

	release()
	wait()
	events, _ := analyze(t, alice, hers, herLesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("once the run ended, the next should start: %+v", last)
	}
}

func TestAReaderRunsOneAnalysisAtATime(t *testing.T) {
	open, release := gate(t)
	m := newScriptedModel(gated(open), texts(goodAnswer), texts(goodAnswer))
	a := aiApp(t, m)
	bob, alice := a.user("bob"), a.user("alice")
	id, lesson := analysisColumn(t, bob)

	hers, herLesson := analysisColumn(t, alice)

	wait := running(t, m, bob, id, lesson)
	_, r := analyze(t, bob, id, lesson, agentQuote)
	expectError(t, r, http.StatusConflict, "run_in_progress")
	// another reader is not held up
	if events, _ := analyze(t, alice, hers, herLesson, agentQuote); lastEvent(t, events).name != "done" {
		t.Fatalf("another reader's run: %+v", lastEvent(t, events))
	}

	release()
	wait()
	if events, _ := analyze(t, bob, id, lesson, agentQuote); lastEvent(t, events).name != "done" {
		t.Fatalf("once the first ended, the reader can start another: %+v", lastEvent(t, events))
	}
}

// A model whose context window is 40,000 tokens leaves 25,904 for the
// lesson (its hard limit: the window less 4,096 for the answer and a
// quarter for tool results), and asks the reader to choose past half that.
const smallWindow = 40_000

// longLessonColumn is a column whose first lesson has n sections of
// size characters each; the selection is in the third.
func longLessonColumn(t *testing.T, c *client, n, size int) (courseID, lessonID int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# 01｜用户输入规范化\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "## 小节%d\n\n正文%d起。%s\n\n", i, i, strings.Repeat("字", size))
		if i == 3 {
			b.WriteString("这里的 Agent 指的是把目标拆成步骤的程序。\n\n")
		}
	}
	files := columnFiles(columnManifest())
	files["01_用户输入.md"] = b.String()
	courseID = uploadCourse(t, c, files)
	return courseID, lessonIDs(t, c, courseID)["l01"]
}

func withLength(choice string) map[string]any {
	body := map[string]any{"length": choice}
	for k, v := range agentQuote {
		body[k] = v
	}
	return body
}

func TestALongLessonNeedsTheReadersChoiceOfHowMuchToSend(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts(goodAnswer))
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	mapModel(t, a, map[string]any{"searchRoute": "off", "contextWindow": smallWindow}, false)
	bob := a.user("bob")
	id, lesson := longLessonColumn(t, bob, 5, 4000)

	_, r := analyze(t, bob, id, lesson, agentQuote)
	expectError(t, r, http.StatusUnprocessableEntity, "length_choice_required")
	length, _ := r.json()["error"].(map[string]any)["length"].(map[string]any)
	if length["soft"] != 12952.0 || length["hard"] != 25904.0 || length["full"] != true {
		t.Fatalf("length = %v", length)
	}
	if len(m.calls()) != 0 {
		t.Fatal("the model was called without a choice")
	}

	events, _ := analyze(t, bob, id, lesson, withLength("related"))
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("with a choice: %+v", last)
	}
	ctx := m.calls()[0].Messages[1].Content
	for _, want := range []string{"只看相关小节", "## 本讲小节目录", "- 小节1", "- 小节5", "正文2起", "正文3起", "<selected>Agent</selected>", "正文4起"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("the related sections should hold %q", want)
		}
	}
	for _, not := range []string{"正文1起", "正文5起"} {
		if strings.Contains(ctx, not) {
			t.Errorf("sections away from the selection were sent: %q", not)
		}
	}

	analyze(t, bob, id, lesson, withLength("full"))
	if ctx := m.calls()[1].Messages[1].Content; !strings.Contains(ctx, "正文1起") || !strings.Contains(ctx, "正文5起") {
		t.Error("the whole lesson should be sent")
	}
}

func TestASelectionAboveTheFirstSectionTakesWhatIsThereAndTheFirstSection(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer))
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	mapModel(t, a, map[string]any{"searchRoute": "off", "contextWindow": smallWindow}, false)
	bob := a.user("bob")
	var b strings.Builder
	b.WriteString("# 01｜用户输入规范化\n\n这里的 Agent 指的是把目标拆成步骤的程序。\n\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "## 小节%d\n\n正文%d起。%s\n\n", i, i, strings.Repeat("字", 4000))
	}
	files := columnFiles(columnManifest())
	files["01_用户输入.md"] = b.String()
	id := uploadCourse(t, bob, files)
	lesson := lessonIDs(t, bob, id)["l01"]

	if events, _ := analyze(t, bob, id, lesson, withLength("related")); lastEvent(t, events).name != "done" {
		t.Fatalf("related, above the first section: %+v", lastEvent(t, events))
	}
	ctx := m.calls()[0].Messages[1].Content
	if !strings.Contains(ctx, "<selected>Agent</selected>") || !strings.Contains(ctx, "正文1起") || strings.Contains(ctx, "正文2起") {
		t.Fatal("the text above the first section and the first section should be sent, and no more")
	}
}

func TestPastTheHardLimitTheWholeLessonCannotBeChosen(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer))
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	mapModel(t, a, map[string]any{"searchRoute": "off", "contextWindow": smallWindow}, false)
	bob := a.user("bob")
	id, lesson := longLessonColumn(t, bob, 8, 4000)

	_, r := analyze(t, bob, id, lesson, agentQuote)
	length, _ := r.json()["error"].(map[string]any)["length"].(map[string]any)
	if length["full"] != false {
		t.Fatalf("length = %v, want the whole lesson out of reach", length)
	}
	_, r = analyze(t, bob, id, lesson, withLength("full"))
	expectError(t, r, http.StatusUnprocessableEntity, "context_too_long")
	if len(m.calls()) != 0 {
		t.Fatal("the model was called")
	}
	if events, _ := analyze(t, bob, id, lesson, withLength("related")); lastEvent(t, events).name != "done" {
		t.Fatalf("the related sections still fit: %+v", lastEvent(t, events))
	}
}

func TestAShortLessonNeedsNoChoice(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer))
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	mapModel(t, a, map[string]any{"searchRoute": "off", "contextWindow": smallWindow}, false)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	if events, _ := analyze(t, bob, id, lesson, agentQuote); lastEvent(t, events).name != "done" {
		t.Fatalf("a short lesson: %+v", lastEvent(t, events))
	}
}

func TestInputPastTheContextWindowIsRefusedBeforeTheModelIsCalled(t *testing.T) {
	m := newScriptedModel()
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	mapModel(t, a, map[string]any{"searchRoute": "off", "contextWindow": 3000}, false)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	_, r := analyze(t, bob, id, lesson, withLength("related"))
	expectError(t, r, http.StatusUnprocessableEntity, "context_too_long")
	if len(m.calls()) != 0 {
		t.Fatal("the model was called")
	}
}

func TestAnUnknownLengthChoiceIsABadRequest(t *testing.T) {
	a := aiApp(t, newScriptedModel())
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)
	_, r := analyze(t, bob, id, lesson, withLength("half"))
	expectError(t, r, http.StatusBadRequest, "bad_request")
}
