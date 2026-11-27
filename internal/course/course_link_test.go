package course_test

import (
	"encoding/json"
	"testing"

	"github.com/Bank5y/keine/internal/course"
)

func TestALinkKeptBeforeLinksWereToSectionsReadsItsConceptAsItsLabel(t *testing.T) {
	var l course.CourseLink
	if err := json.Unmarshal([]byte(`{"concept":"意图识别","note":"相关","lesson":{"id":3,"key":"l02","title":"02"},"section":null}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.Label != "意图识别" || l.Note != "相关" || l.Lesson.Key != "l02" || l.Section != nil {
		t.Fatalf("read %+v", l)
	}
	if err := json.Unmarshal([]byte(`{"label":"三层意图","note":"相关","lesson":{"id":3,"key":"l02","title":"02"},"section":{"id":"a","title":"三层意图"}}`), &l); err != nil || l.Label != "三层意图" || l.Section.ID != "a" {
		t.Fatalf("read %+v, %v", l, err)
	}
}
