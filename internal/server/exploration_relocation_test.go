package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// analysisLesson is analysisColumn's l01 text; the analyzed "Agent" is the
// one in its last line.
const analysisLesson = "# 01｜用户输入规范化\n\n" +
	"Agent 不是一个新概念，但在大模型出现之后，它第一次变得**真正可用**。\n\n" +
	"## 一个最小的例子\n\n" +
	"这里的 Agent 指的是把目标拆成步骤的程序。\n"

// analysisFiles is analysisColumn's package with l01's text replaced.
func analysisFiles(l01 string) map[string]string {
	files := columnFiles(columnManifest())
	files["01_用户输入.md"] = l01
	return files
}

// pinnedExploration has bob analyze and pin agentQuote on analysisColumn.
func pinnedExploration(t *testing.T, bob *client) (courseID, lessonID, explorationID int) {
	t.Helper()
	courseID, lessonID = analysisColumn(t, bob)
	r := pin(bob, courseID, lessonID, analysisID(t, bob, courseID, lessonID, agentQuote))
	expectStatus(t, r, http.StatusCreated)
	return courseID, lessonID, int(r.json()["id"].(float64))
}

// explorationRow reads, past the API, what an exploration keeps that no
// endpoint shows: its state, whether it remembers the text it was shown
// in, and its snapshot.
type explorationRow struct {
	state       string
	previousSet bool
	snapshot    string
}

func readExploration(t *testing.T, a *app, id int) (explorationRow, bool) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var e explorationRow
	err = conn.QueryRow(ctx, "SELECT state, previous_text_id IS NOT NULL, snapshot::text FROM explorations WHERE id = $1", id).
		Scan(&e.state, &e.previousSet, &e.snapshot)
	if err == pgx.ErrNoRows {
		return e, false
	}
	if err != nil {
		t.Fatalf("exploration: %v", err)
	}
	return e, true
}

func TestAnExplorationWhoseTextIsGoneAwaitsMigration(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	id, lesson, exp := pinnedExploration(t, bob)
	before, _ := readExploration(t, a, exp)

	// the sentence is reworded: "Agent" is still in the lesson, but not here
	bob.mustOK(updateCourse(t, bob, id, analysisFiles(strings.Replace(analysisLesson, "这里的 Agent 指的是", "这里说的智能体指的是", 1))))

	if list := explorations(t, bob, id, lesson); len(list) != 0 {
		t.Fatalf("an exploration that cannot be put back leaves the text, got %v", list)
	}
	after, ok := readExploration(t, a, exp)
	if !ok || after.state != "pending" || !after.previousSet {
		t.Fatalf("expected it kept, pending against the text it was shown in, got %+v (found %v)", after, ok)
	}
	if after.snapshot != before.snapshot {
		t.Fatalf("the snapshot is never rewritten:\nbefore %s\nafter  %s", before.snapshot, after.snapshot)
	}
}

func TestAnExplorationFollowsItsTextThroughACourseUpdate(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	id, lesson, exp := pinnedExploration(t, bob)
	was := textRange(explorations(t, bob, id, lesson)[0])

	// a paragraph of 5 visible characters lands before it
	bob.mustOK(updateCourse(t, bob, id, analysisFiles(strings.Replace(analysisLesson, "## 一个最小的例子", "新加一段。\n\n## 一个最小的例子", 1))))

	list := explorations(t, bob, id, lesson)
	if len(list) != 1 || int(list[0]["id"].(float64)) != exp {
		t.Fatalf("expected the exploration still shown, got %v", list)
	}
	if now := textRange(list[0]); now == nil || now[0] != was[0]+5 || now[1] != was[1]+5 {
		t.Fatalf("expected the range moved by 5 from %v, got %v", was, now)
	}
}

func TestTheExplorationsOfADeletedLessonBecomeExcerpts(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	id, _, exp := pinnedExploration(t, bob)

	m := columnManifest()
	intent := m["chapters"].([]any)[1].(map[string]any)
	intent["lessons"] = intent["lessons"].([]any)[1:] // l01 goes
	files := columnFiles(m)
	delete(files, "01_用户输入.md")
	delete(files, "images/1/a.png")
	bob.mustOK(updateCourse(t, bob, id, files))

	if e, ok := readExploration(t, a, exp); !ok || e.state != "excerpt" {
		t.Fatalf("expected the exploration kept as an excerpt, got %+v (found %v)", e, ok)
	}
}

func TestThePendingNoticeCountsAnnotationsOnly(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	id, lesson, _ := pinnedExploration(t, bob)
	annotate(t, bob, id, lesson, map[string]any{"style": "underline", "quote": "指的是", "prefix": "这里的 Agent ", "suffix": "把目标"})

	bob.mustOK(updateCourse(t, bob, id, analysisFiles(strings.Replace(analysisLesson, "这里的 Agent 指的是", "这里说的智能体意思是", 1))))

	if p := onlyNotification(t, bob, "annotations_pending"); p["annotations"] != float64(1) || p["lessons"] != float64(1) {
		t.Fatalf("the exploration gone pending too is not counted: %v", p)
	}
}
