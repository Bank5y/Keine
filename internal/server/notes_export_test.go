package server_test

import (
	"fmt"
	"mime"
	"net/http"
	"testing"
)

func exportNotes(c *client, courseID int) resp {
	return c.do("GET", fmt.Sprintf("/api/my/annotations/export?course=%d", courseID), nil)
}

func TestNotesExportAsMarkdownLessonByLessonInTextOrder(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id, l00 := annotatedColumn(t, alice)
	lessons := lessonIDs(t, alice, id)
	annotate(t, alice, id, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})
	annotate(t, alice, id, l00, map[string]any{"style": "underline", "quote": "完成任务"})
	annotate(t, alice, id, l00, map[string]any{"style": "highlight", "color": "green", "quote": "会思考", "note": "要点\n\n第二段笔记"})
	// another course's annotations stay out of this export
	other, ol := secondColumn(t, alice)
	annotate(t, alice, other, ol["l00"], map[string]any{"style": "underline", "quote": "你好"})

	r := exportNotes(alice, id)
	expectStatus(t, r, http.StatusOK)
	if ct, _, _ := mime.ParseMediaType(r.header.Get("Content-Type")); ct != "text/markdown" {
		t.Fatalf("unexpected content type %q", r.header.Get("Content-Type"))
	}
	if d, p, _ := mime.ParseMediaType(r.header.Get("Content-Disposition")); d != "attachment" || p["filename"] != "Agent 系统设计.md" {
		t.Fatalf("downloads as a .md file named after the course: %q", r.header.Get("Content-Disposition"))
	}
	want := "# Agent 系统设计\n\n" +
		"## 开篇 · 开篇词\n\n" +
		"> 会思考\n\n要点\n\n第二段笔记\n\n" +
		"> 完成任务\n\n" +
		"## 意图识别 · 02｜三层意图识别\n\n" +
		"> 02\n"
	if got := string(r.body); got != want {
		t.Fatalf("unexpected export:\n%s\nwant:\n%s", got, want)
	}
}

func TestNotesExportKeepsDetachedAnnotationsWithTheirState(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	files := columnFiles(columnManifest())
	files["01_用户输入.md"] = "# 01\n\n第一行\n\n第二行\n"
	id := uploadCourse(t, alice, files)
	lessons := lessonIDs(t, alice, id)
	annotate(t, alice, id, lessons["l00"], map[string]any{"style": "underline", "quote": "你好", "note": "开篇的笔记"})
	annotate(t, alice, id, lessons["l01"], map[string]any{"style": "underline", "quote": "第一行\n\n第二行"})
	annotate(t, alice, id, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})

	// the update drops the opening lesson and rewrites l01, leaving its
	// annotation pending
	_, next := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, next))

	r := exportNotes(alice, id)
	expectStatus(t, r, http.StatusOK)
	want := "# Agent 系统设计 (第二版)\n\n" +
		"## 意图识别 · 01｜用户输入规范化 (修订)\n\n" +
		"> 第一行\n>\n> 第二行\n\n*待迁移*\n\n" +
		"## 意图识别 · 02｜三层意图识别\n\n" +
		"> 02\n\n" +
		"## 已删除的讲\n\n" +
		"> 你好\n\n*原文摘录：原文已删除*\n\n开篇的笔记\n"
	if got := string(r.body); got != want {
		t.Fatalf("unexpected export:\n%s\nwant:\n%s", got, want)
	}
}

func TestNotesExportIsOnlyForACourseOfMine(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id, l00 := annotatedColumn(t, alice)
	annotate(t, alice, id, l00, map[string]any{"style": "underline", "quote": "完成任务"})

	expectStatus(t, exportNotes(a.user("bob"), id), http.StatusNotFound)
	expectStatus(t, alice.do("GET", "/api/my/annotations/export", nil), http.StatusBadRequest)
	expectStatus(t, alice.do("GET", "/api/my/annotations/export?course=x", nil), http.StatusBadRequest)
	expectStatus(t, a.client().do("GET", fmt.Sprintf("/api/my/annotations/export?course=%d", id), nil), http.StatusUnauthorized)
}
