package ui

import (
	"testing"
)

func TestReviewBadge(t *testing.T) {
	if capped(9) != "9" || capped(10) != "9+" {
		t.Fatal("counts above 9 show as 9+")
	}
	m := setup(t, demoOpts(), 120, 30)
	if !m.alertSet || m.pulse != 0 {
		t.Fatal("the count climbing as PRs first load must not pulse")
	}
	n := m.profileStats(clock()).requested
	m.alertBase = n - 1
	if cmd := m.watchReviews(); cmd == nil || m.pulse != pulseBeats {
		t.Fatal("a rise in review requests should pulse the badge")
	}
	dim := m.badges(false)
	m.onPulse()
	if m.badges(false) == dim {
		t.Fatal("the pulse should alternate the badge's look")
	}
	for m.pulse > 0 {
		m.onPulse()
	}
	if m.watchReviews() != nil || m.pulse != 0 {
		t.Fatal("an unchanged count should not pulse again")
	}
	if m.View().WindowTitle != "git-tui ("+capped(n)+")" {
		t.Fatalf("the window title should carry the count: %q", m.View().WindowTitle)
	}
}
