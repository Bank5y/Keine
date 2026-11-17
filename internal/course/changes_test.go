package course

import "testing"

func TestChapterAndLessonKeysDoNotMix(t *testing.T) {
	text := func(k string) Lesson { return Lesson{Key: k, Title: "讲" + k, File: k + ".md"} }
	before := Manifest{Chapters: []Chapter{
		{Key: "1", Title: "一", Lessons: []Lesson{text("1")}},
		{Key: "2", Title: "二", Lessons: []Lesson{text("2"), text("3")}},
		{Key: "3", Title: "三", Lessons: []Lesson{text("4")}},
	}}
	// lesson "2" and chapter "3" go; chapter "2" and lesson "3" stay
	after := Manifest{Chapters: []Chapter{
		{Key: "1", Title: "一", Lessons: []Lesson{text("1")}},
		{Key: "2", Title: "二", Lessons: []Lesson{text("3")}},
	}}

	c := outlineChanges(before, after, nil)

	if c.Removed != 2 {
		t.Fatalf("both removed lessons count, got %d", c.Removed)
	}
	var removedChapter bool
	for _, ch := range c.Chapters {
		if ch.Key == "3" && len(ch.Changes) == 1 && ch.Changes[0] == ChangeRemoved {
			removedChapter = true
		}
	}
	if !removedChapter {
		t.Fatalf("the removed chapter is listed: %+v", c.Chapters)
	}
}
