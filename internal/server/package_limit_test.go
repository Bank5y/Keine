package server_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// bombFiles is a small course whose one lesson compresses to almost
// nothing but unpacks to 4 KiB.
func bombFiles() map[string]string {
	return map[string]string{
		"course.json": manifestJSON(map[string]any{
			"version": 1, "title": "小课", "type": "column",
			"chapters": []any{map[string]any{"id": "c", "title": "章", "lessons": []any{map[string]any{"id": "l", "title": "讲", "file": "l.md"}}}},
		}),
		"l.md": "# l\n" + strings.Repeat("a", 4<<10),
	}
}

// expectUnpackedTooLarge checks a package was refused for what it unpacks
// to, not for what it weighs: both answer package_too_large.
func expectUnpackedTooLarge(t *testing.T, r resp) {
	t.Helper()
	expectError(t, r, http.StatusRequestEntityTooLarge, "package_too_large")
	if !strings.Contains(string(r.body), "解压后") {
		t.Fatalf("refused for its unpacked size: %s", r.body)
	}
}

func TestACoursePackageMayNotUnpackPastTheUploadLimit(t *testing.T) {
	a := newApp(t, withUploadLimit(1<<10))
	alice := a.user("alice")

	expectUnpackedTooLarge(t, alice.upload("/api/courses", zipOf(t, bombFiles())))
	if left := a.storedKeys("courses/"); len(left) != 0 {
		t.Fatalf("nothing of it is stored: %v", left)
	}
}

func TestAPackageUpdateMayNotUnpackPastTheUploadLimit(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	prefix := fmt.Sprintf("courses/%d/", id)
	before := a.storedKeys(prefix)
	a = a.restart(withUploadLimit(1 << 10))
	alice = a.as("alice", "alice-password")

	expectUnpackedTooLarge(t, updateCourse(t, alice, id, bombFiles()))
	if after := a.storedKeys(prefix); !slices.Equal(after, before) {
		t.Fatalf("nothing of it is stored: was %v, now %v", before, after)
	}
}

func TestAnUpdateRequestMayNotUnpackPastTheUploadLimit(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	a.user("bob")
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	prefix := fmt.Sprintf("courses/%d/", id)
	before := a.storedKeys(prefix)
	a = a.restart(withUploadLimit(1 << 10))
	bob := a.as("bob", "bob-password")

	expectUnpackedTooLarge(t, submitUpdate(t, bob, id, bombFiles()))
	if after := a.storedKeys(prefix); !slices.Equal(after, before) {
		t.Fatalf("nothing of it is stored: was %v, now %v", before, after)
	}
}
