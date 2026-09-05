package selector

import "testing"

func TestRejectIncompleteSelectors(t *testing.T) {
	for _, s := range []string{"", "best+", "/best", "best[ext=mp4", "best[]", "best[ext=mp4]junk", "best[[ext=mp4]]", "best[ext=]", "best[height=abc]"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestExtensionInequalityPreserved(t *testing.T) {
	s, err := Parse("best[ext!=mp4]")
	if err != nil {
		t.Fatal(err)
	}
	if s.Fallbacks[0][0].Filters[1].Op != "!=" {
		t.Fatal("lost inequality")
	}
}
