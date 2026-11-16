package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// rewordedOpening is openingText with the tool sentence reworded and the
// second paragraph gone, so annotations on either await migration.
func rewordedOpening() string {
	revised := strings.Replace(openingText, "[调用工具](https://example.com)", "使用工具", 1)
	return strings.Replace(revised, "第二段也提到 Agent，以及 `tool_call` 这个字段。\n\n", "", 1)
}

func TestAnnotatorsAreToldHowManyAnnotationsAwaitMigration(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	subscribe(t, bob, id)
	annotate(t, bob, id, lessons["l01"], map[string]any{"style": "underline", "quote": "官网", "prefix": "见", "suffix": "。"})
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "highlight", "color": "blue",
		"quote": "Agent", "prefix": "第二段也提到 ", "suffix": "，以及 tool_call"})
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "会思考", "prefix": "Agent 是一个", "suffix": "的程序"})

	files := annotatedFiles(rewordedOpening())
	files["01_用户输入.md"] = "# 01\n\n![](images/1/a.png)\n\n全新的说明。\n"
	approveUpdate(t, a, alice, id, files)

	p := onlyNotification(t, bob, "annotations_pending")
	if int(p["courseId"].(float64)) != id || p["courseTitle"] != "Agent 系统设计" ||
		p["lessons"] != float64(2) || p["annotations"] != float64(3) || int(p["lessonId"].(float64)) != lessons["l00"] {
		t.Fatalf("unexpected notification: %v", p)
	}
}

func TestAnnotatorsWhoseAnnotationsAllReattachAreNotTold(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	carol := a.user("carol")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	subscribe(t, carol, id)
	annotate(t, carol, id, lessons["l00"], map[string]any{"style": "underline", "quote": "会思考", "prefix": "Agent 是一个", "suffix": "的程序"})

	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(rewordedOpening())))

	if n := notificationsOfKind(t, carol, "annotations_pending"); len(n) != 0 {
		t.Fatalf("nothing awaits migration, so nothing is told: %v", n)
	}
	onlyNotification(t, carol, "course_updated")
}

func TestFormerSubscribersAreToldOfTheirPendingAnnotations(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	erin := a.user("erin")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	subscribe(t, erin, id)
	annotate(t, erin, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	erin.mustOK(erin.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))

	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(rewordedOpening())))

	p := onlyNotification(t, erin, "annotations_pending")
	if p["lessons"] != float64(1) || p["annotations"] != float64(1) || int(p["lessonId"].(float64)) != lessons["l00"] {
		t.Fatalf("unexpected notification: %v", p)
	}
	if n := notificationsOfKind(t, erin, "course_updated"); len(n) != 0 {
		t.Fatalf("a former subscriber hears of no update otherwise: %v", n)
	}
}

func TestEachUpdateTellsAnAnnotatorAtMostOnce(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	admin := a.admin()
	admin.mustOK(updateCourse(t, admin, id, annotatedFiles(rewordedOpening())))
	onlyNotification(t, bob, "annotations_pending")

	// a later update changing the lesson again leaves the annotation pending
	// as it was: nothing newly awaits migration
	admin.mustOK(updateCourse(t, admin, id, annotatedFiles(rewordedOpening()+"\n又加了一段。\n")))
	onlyNotification(t, bob, "annotations_pending")
}
