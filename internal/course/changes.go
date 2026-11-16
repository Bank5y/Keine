package course

// What a course update does to a chapter or a lesson.
const (
	ChangeAdded    = "added"    // new; for a lesson, also an upcoming lesson getting content
	ChangeRemoved  = "removed"  // gone; for a lesson, also one whose content is taken away
	ChangeModified = "modified" // a kept lesson whose text changed
	ChangeRenamed  = "renamed"  // a kept chapter or lesson under a new title
	ChangeMoved    = "moved"    // a kept lesson in another chapter or out of its order; a chapter out of its order
)

// Changes is what a course update changes, counted and chapter by chapter.
// The counts are of lessons, as subscribers are told them; lessons without
// content (upcoming ones) count only when renamed or moved, and when they
// come or go they are listed with Upcoming set but not counted. Kept counts
// the lessons that have content before and after.
type Changes struct {
	Added    int             `json:"added"`
	Removed  int             `json:"removed"`
	Modified int             `json:"modified"`
	Renamed  int             `json:"renamed"`
	Moved    int             `json:"moved"`
	Kept     int             `json:"kept"`
	Chapters []ChapterChange `json:"chapters"`
}

// ChapterChange is a chapter the update changes or changes lessons of, in
// the order of the new version, followed by the chapters it removes.
// Lessons lists its changed lessons, then those removed from it.
type ChapterChange struct {
	Key           string         `json:"key"`
	Title         string         `json:"title"`
	PreviousTitle string         `json:"previousTitle,omitempty"`
	Changes       []string       `json:"changes"`
	Lessons       []LessonChange `json:"lessons"`
}

type LessonChange struct {
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	PreviousTitle string   `json:"previousTitle,omitempty"`
	FromChapter   string   `json:"fromChapter,omitempty"` // the title of the chapter it moved from
	Upcoming      bool     `json:"upcoming,omitempty"`
	Changes       []string `json:"changes"`
}

// outlineChanges compares two versions of a course chapter by chapter;
// modified names the kept lessons whose text changed.
func outlineChanges(before, after Manifest, modified map[string]bool) Changes {
	type place struct {
		chapter int // in before.Chapters
		index   int // in that chapter
		lesson  Lesson
	}
	oldChapter := map[string]int{}
	oldLesson := map[string]place{}
	for i, ch := range before.Chapters {
		oldChapter[ch.Key] = i
		for j, l := range ch.Lessons {
			oldLesson[l.Key] = place{i, j, l}
		}
	}
	inAfter := map[string]bool{}
	var keptChapters []int
	for _, ch := range after.Chapters {
		inAfter[ch.Key] = true
		if i, ok := oldChapter[ch.Key]; ok {
			keptChapters = append(keptChapters, i)
		}
		for _, l := range ch.Lessons {
			inAfter[l.Key] = true
		}
	}

	c := Changes{Chapters: []ChapterChange{}}
	removedFrom := map[int][]LessonChange{}
	for i, ch := range before.Chapters {
		for _, l := range ch.Lessons {
			if inAfter[l.Key] {
				continue
			}
			if !l.Upcoming() {
				c.Removed++
			}
			removedFrom[i] = append(removedFrom[i], LessonChange{Key: l.Key, Title: l.Title, Upcoming: l.Upcoming(), Changes: []string{ChangeRemoved}})
		}
	}

	chaptersInOrder := inOrder(keptChapters)
	for _, ch := range after.Chapters {
		cc := ChapterChange{Key: ch.Key, Title: ch.Title, Changes: []string{}, Lessons: []LessonChange{}}
		i, keptChapter := oldChapter[ch.Key]
		if !keptChapter {
			cc.Changes = append(cc.Changes, ChangeAdded)
		} else {
			if prev := before.Chapters[i].Title; prev != ch.Title {
				cc.Changes = append(cc.Changes, ChangeRenamed)
				cc.PreviousTitle = prev
			}
			if !chaptersInOrder[i] {
				cc.Changes = append(cc.Changes, ChangeMoved)
			}
		}
		var stayed []int
		for _, l := range ch.Lessons {
			if p, ok := oldLesson[l.Key]; ok && keptChapter && p.chapter == i {
				stayed = append(stayed, p.index)
			}
		}
		lessonsInOrder := inOrder(stayed)
		for _, l := range ch.Lessons {
			lc := LessonChange{Key: l.Key, Title: l.Title, Upcoming: l.Upcoming(), Changes: []string{}}
			p, kept := oldLesson[l.Key]
			hadContent := kept && !p.lesson.Upcoming()
			switch {
			case !kept:
				lc.Changes = append(lc.Changes, ChangeAdded)
				if !l.Upcoming() {
					c.Added++
				}
			case hadContent && l.Upcoming():
				lc.Changes = append(lc.Changes, ChangeRemoved)
				c.Removed++
			case !hadContent && !l.Upcoming():
				lc.Changes = append(lc.Changes, ChangeAdded)
				c.Added++
			default:
				if hadContent {
					c.Kept++
				}
				if modified[l.Key] {
					lc.Changes = append(lc.Changes, ChangeModified)
					c.Modified++
				}
				if p.lesson.Title != l.Title {
					lc.Changes = append(lc.Changes, ChangeRenamed)
					lc.PreviousTitle = p.lesson.Title
					c.Renamed++
				}
				switch {
				case !keptChapter || p.chapter != i:
					lc.FromChapter = before.Chapters[p.chapter].Title
					fallthrough
				case !lessonsInOrder[p.index]:
					lc.Changes = append(lc.Changes, ChangeMoved)
					c.Moved++
				}
			}
			if len(lc.Changes) > 0 {
				cc.Lessons = append(cc.Lessons, lc)
			}
		}
		if keptChapter {
			cc.Lessons = append(cc.Lessons, removedFrom[i]...)
		}
		if len(cc.Changes) > 0 || len(cc.Lessons) > 0 {
			c.Chapters = append(c.Chapters, cc)
		}
	}
	for i, ch := range before.Chapters {
		if !inAfter[ch.Key] {
			c.Chapters = append(c.Chapters, ChapterChange{Key: ch.Key, Title: ch.Title, Changes: []string{ChangeRemoved},
				Lessons: append([]LessonChange{}, removedFrom[i]...)})
		}
	}
	return c
}

// inOrder takes the old positions of kept items in their new order and
// picks a longest run of them that kept its relative order; the others are
// the ones that moved. It returns the old positions that stayed in order.
func inOrder(seq []int) map[int]bool {
	n := len(seq)
	length := make([]int, n) // longest increasing run ending at i
	prev := make([]int, n)
	best := -1
	for i := range seq {
		length[i], prev[i] = 1, -1
		for j := 0; j < i; j++ {
			if seq[j] < seq[i] && length[j]+1 > length[i] {
				length[i], prev[i] = length[j]+1, j
			}
		}
		if best < 0 || length[i] >= length[best] { // on a tie, the item pulled ahead is the one that moved

			best = i
		}
	}
	out := map[int]bool{}
	for i := best; i >= 0; i = prev[i] {
		out[seq[i]] = true
	}
	return out
}
