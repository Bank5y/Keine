package assistant_test

import (
	"slices"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
)

func TestCitedReadsNumbersAsTheReplyShowsThem(t *testing.T) {
	for md, want := range map[string][]int{
		"Raft 的论文 [1]，Paxos 见 [2][3]。": {1, 2, 3},
		"两篇都讲了选举 [1, 2]，见 [2]。":        {1, 2},
		"论文[4]": {4},
		// an index after a name is code, not a citation
		"取 a[1] 和 f(x)[2]，再看 m[0][1]": nil,
		"列表写作 `b[5]`。":                nil,
	} {
		if got := assistant.Cited(md); !slices.Equal(got, want) {
			t.Errorf("Cited(%q) = %v, want %v", md, got, want)
		}
	}
}
