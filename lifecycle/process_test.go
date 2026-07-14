package lifecycle

import "testing"

func TestProcessPresentationIsExplicitAndInherited(t *testing.T) {
	headless, err := resolveProcessSpec(ProcessSpec{
		Executable: "/test/process", Env: []string{"PATH=/bin"}, Presentation: PresentationHeadless,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolvePresentation(headless.Env)
	if err != nil || resolved != PresentationHeadless {
		t.Fatalf("presentation=%q error=%v env=%v", resolved, err, headless.Env)
	}
	inherited, err := resolveProcessSpec(ProcessSpec{Executable: "/test/child", Env: headless.Env})
	if err != nil {
		t.Fatal(err)
	}
	if inherited.Presentation != PresentationHeadless {
		t.Fatalf("inherited presentation=%q", inherited.Presentation)
	}
	if _, err := ResolvePresentation([]string{presentationEnvironment + "=escaped"}); err == nil {
		t.Fatal("invalid inherited presentation was accepted")
	}
}
