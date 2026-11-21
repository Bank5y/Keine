package course

import "testing"

func TestAConceptIsFoundByItsNameOrAnAliasAsWritten(t *testing.T) {
	x := ConceptIndex{Status: IndexBuilt, Entries: []ConceptEntry{
		{Name: "任期", Aliases: []string{"term"}},
		{Name: "Leader Election", Aliases: []string{"领导者选举"}},
		{Name: "日志复制"},
	}}
	for term, want := range map[string]string{
		"任期":              "任期",
		"Term":            "任期",
		"ｔｅｒｍ":            "任期",
		"leader election": "Leader Election",
		"LeaderElection":  "Leader Election",
		"领导者选举":           "Leader Election",
	} {
		got := x.Find(term)
		if len(got) != 1 || got[0].Name != want {
			t.Errorf("Find(%q) = %v, want %s", term, got, want)
		}
	}
	if got := x.Find("日志"); len(got) != 0 {
		t.Errorf("a part of a name is not the name: %v", got)
	}
}
