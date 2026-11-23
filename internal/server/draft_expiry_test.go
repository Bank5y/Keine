package server_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/server"
)

const day = 24 * time.Hour

// fakeClock is the app's time, moved by the test instead of waited for.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newClockedApp is an app whose time the returned clock sets.
func newClockedApp(t *testing.T) (*app, *fakeClock) {
	clock := &fakeClock{now: time.Now()}
	a := newApp(t, func(o *server.Options) { o.Now = clock.Now })
	return a, clock
}

// sweep runs the periodic draft expiry once, at the clock's time.
func (a *app) sweep() {
	a.t.Helper()
	if err := a.server.SweepDrafts(context.Background()); err != nil {
		a.t.Fatalf("sweep drafts: %v", err)
	}
}

func TestADraftUntouchedFor25DaysRemindsItsOwnerOnce(t *testing.T) {
	t.Parallel()
	a, clock := newClockedApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addChapter", "title": "计划"}))

	clock.advance(24 * day)
	a.sweep()
	if n := notificationsOfKind(t, alice, "draft_expiring"); len(n) != 0 {
		t.Fatalf("no reminder before day 25: %v", n)
	}

	clock.advance(day)
	a.sweep()
	p := onlyNotification(t, alice, "draft_expiring")
	if p["courseId"] != float64(id) || p["courseTitle"] == "" {
		t.Fatalf("the reminder names the course: %v", p)
	}

	clock.advance(day)
	a.sweep()
	onlyNotification(t, alice, "draft_expiring")
	if len(draftOps(t, alice, id)) != 1 {
		t.Fatal("a reminded draft is kept until it expires")
	}
}

// draftAreaKeys lists the stored files of the course's draft areas.
func draftAreaKeys(a *app, id int) []string {
	var out []string
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if dir := strings.Split(k, "/")[2]; strings.HasPrefix(dir, "d") {
			out = append(out, k)
		}
	}
	return out
}

func TestADraftUntouchedFor30DaysIsDeletedWithItsFiles(t *testing.T) {
	t.Parallel()
	a, clock := newClockedApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	u := uploadFiles(t, alice, id, map[string]string{"03_记忆.md": "# 03\n"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03｜记忆", "content": map[string]any{"upload": u}}))
	if len(draftAreaKeys(a, id)) == 0 {
		t.Fatal("the upload is kept in the draft's area")
	}

	clock.advance(30*day - time.Minute)
	a.sweep()
	if len(draftOps(t, alice, id)) != 1 {
		t.Fatal("a draft is kept until 30 days have passed")
	}

	clock.advance(time.Minute)
	a.sweep()
	if ops := draftOps(t, alice, id); len(ops) != 0 {
		t.Fatalf("the expired draft is gone: %v", ops)
	}
	if keys := draftAreaKeys(a, id); len(keys) != 0 {
		t.Fatalf("the expired draft's files are gone: %v", keys)
	}
	expectOutline(t, courseOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")
}

func TestWorkingOnADraftStartsItsTimeOver(t *testing.T) {
	t.Parallel()
	a, clock := newClockedApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addChapter", "title": "计划"}))

	clock.advance(26 * day)
	a.sweep()
	onlyNotification(t, alice, "draft_expiring")
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addChapter", "title": "总结"}))

	clock.advance(24 * day)
	a.sweep()
	if len(draftOps(t, alice, id)) != 2 || len(notificationsOfKind(t, alice, "draft_expiring")) != 1 {
		t.Fatal("after new work the draft is neither reminded of nor deleted for another 25 days")
	}

	clock.advance(day)
	a.sweep()
	if n := notificationsOfKind(t, alice, "draft_expiring"); len(n) != 2 {
		t.Fatalf("a new idle spell brings a new reminder: %v", n)
	}

	alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil))
	clock.advance(29 * day)
	a.sweep()
	if len(draftOps(t, alice, id)) != 1 {
		t.Fatal("undoing counts as work on the draft")
	}
	clock.advance(day)
	a.sweep()
	if len(draftOps(t, alice, id)) != 0 {
		t.Fatal("the draft expires 30 days after its last change")
	}
}

// deleteUser removes a user's account with everything that cascades from it,
// as an operator would in the database.
func deleteUser(t *testing.T, a *app, name string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DELETE FROM users WHERE username = $1", name); err != nil {
		t.Fatal(err)
	}
}

func TestTheSweepRemovesFilesOfDraftsWhoseOwnerIsGone(t *testing.T) {
	t.Parallel()
	a, _ := newClockedApp(t)
	alice := a.user("alice")
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	bob := a.user("bob")
	uploadFiles(t, bob, id, map[string]string{"bob.md": "# bob\n"})
	carol := a.user("carol")
	uploadFiles(t, carol, id, map[string]string{"carol.md": "# carol\n"})

	deleteUser(t, a, "bob")
	a.sweep()

	keys := draftAreaKeys(a, id)
	if len(keys) != 1 || !strings.HasSuffix(keys[0], "/carol.md") {
		t.Fatalf("only the live draft keeps its files: %v", keys)
	}
}
