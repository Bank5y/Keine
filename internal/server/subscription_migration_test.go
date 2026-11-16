package server_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestEnrollmentsBecomeSubscriptions starts from the schema before
// subscriptions existed: every course in my courses was an enrollment, and
// removing one only marked it removed.
func TestEnrollmentsBecomeSubscriptions(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	kept := publishAs(t, a, columnFiles(columnManifest()))
	removed := publishAs(t, a, columnFiles(columnManifest()))
	for _, id := range []int{kept, removed} {
		visit(t, bob, id, lessonIDs(t, bob, id)["l01"])
	}
	rollBackSubscriptions(t, a.dbURL, `UPDATE enrollments SET removed_at = now() WHERE course_id = `+fmt.Sprint(removed))

	b := a.restart()
	bob = b.as("bob", "bob-password")

	if ids := myCourseIDs(t, bob); !contains(ids, kept) || contains(ids, removed) {
		t.Fatalf("enrolled courses stay, removed ones do not: %v", ids)
	}
	if p := courseProgress(t, bob, removed); p["lastLessonId"] != float64(lessonIDs(t, bob, removed)["l01"]) {
		t.Fatalf("the removed course keeps its reading record: %v", p)
	}
}

// rollBackSubscriptions turns the database back into its shape before the
// subscriptions migration, enrolling every reader, then runs setup on it.
func rollBackSubscriptions(t *testing.T, dbURL, setup string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, q := range []string{
		`ALTER TABLE course_readers RENAME TO enrollments`,
		`ALTER TABLE enrollments DROP COLUMN subscribed_at`,
		`ALTER TABLE enrollments ADD COLUMN removed_at timestamptz`,
		`DELETE FROM schema_migrations WHERE name = '0009_subscriptions.sql'`,
		setup,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}
