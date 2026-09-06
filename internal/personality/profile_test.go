package personality

import "testing"

func TestBuiltinsAndThinkTime(t *testing.T) {
	if len(Builtins()) != 6 {
		t.Fatalf("profiles=%d", len(Builtins()))
	}
	profile, err := Resolve("tricky")
	if err != nil {
		t.Fatal(err)
	}
	first := ThinkTime(profile, 42, .8)
	second := ThinkTime(profile, 42, .8)
	if first != second || first < profile.ThinkMin || first > profile.ThinkMax {
		t.Fatalf("think times first=%v second=%v profile=%+v", first, second, profile)
	}
	if _, err := Resolve("missing"); err == nil {
		t.Fatal("expected unknown personality error")
	}
}
