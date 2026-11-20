//go:build desktop

package desktop

import (
	"context"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

func desktopTabs(t *testing.T, u *Window) *container.AppTabs {
	t.Helper()
	var find func(fyne.CanvasObject) *container.AppTabs
	find = func(obj fyne.CanvasObject) *container.AppTabs {
		if tabs, ok := obj.(*container.AppTabs); ok {
			return tabs
		}
		if c, ok := obj.(*fyne.Container); ok {
			for _, child := range c.Objects {
				if tabs := find(child); tabs != nil {
					return tabs
				}
			}
		}
		return nil
	}
	tabs := find(u.Window.Content())
	if tabs == nil {
		t.Fatal("missing desktop tabs")
	}
	return tabs
}

func TestPolicyWheelEscapesEntriesAtPageBottom(t *testing.T) {
	u := testWindow(t)
	u.Show()
	tabs := desktopTabs(t, u)
	tabs.SelectIndex(3)
	page := tabs.Items[3].Content.(*container.Scroll)
	for _, item := range []struct {
		name  string
		entry fyne.CanvasObject
	}{
		{"domains", u.domains}, {"targets", u.targets}, {"timeout", u.timeout},
	} {
		t.Run(item.name, func(t *testing.T) {
			page.ScrollToBottom()
			before := page.Offset.Y
			if before <= 0 {
				t.Fatal("test page does not overflow")
			}
			pos := u.App.Driver().AbsolutePositionForObject(item.entry).Add(fyne.NewPos(20, item.entry.Size().Height/2))
			pagePos := u.App.Driver().AbsolutePositionForObject(page)
			if pos.Y < pagePos.Y || pos.Y >= pagePos.Y+page.Size().Height {
				t.Fatal("test entry is outside viewport", pos, pagePos, page.Size())
			}
			// Use the same hit testing as the desktop driver, not page.Scrolled.
			test.Scroll(u.Window.Canvas(), pos, 0, 48)
			if page.Offset.Y >= before {
				t.Fatalf("wheel over %s was swallowed at page bottom: %v -> %v", item.name, before, page.Offset.Y)
			}
		})
	}
}

func entryScroll(t *testing.T, entry *formEntry) *container.Scroll {
	t.Helper()
	for _, obj := range test.WidgetRenderer(entry).Objects() {
		if scroll, ok := obj.(*container.Scroll); ok {
			return scroll
		}
	}
	t.Fatal("missing entry scroll container")
	return nil
}

func TestMultilineWheelChainsAtBothBoundaries(t *testing.T) {
	u := testWindow(t)
	u.Show()
	tabs := desktopTabs(t, u)
	tabs.SelectIndex(3)
	page := tabs.Items[3].Content.(*container.Scroll)
	u.domains.SetText(strings.Repeat("example.local\n", 30))
	inner := entryScroll(t, u.domains)
	position := func() fyne.Position {
		return u.App.Driver().AbsolutePositionForObject(u.domains).Add(fyne.NewPos(20, 20))
	}
	page.ScrollToBottom()
	bottom := page.Offset.Y
	inner.ScrollToTop()
	test.Scroll(u.Window.Canvas(), position(), 0, -32)
	if inner.Offset.Y <= 0 || page.Offset.Y != bottom {
		t.Fatal("overflowing text must scroll before the page", inner.Offset, page.Offset)
	}
	inner.ScrollToBottom()
	page.ScrollToOffset(fyne.NewPos(0, bottom-24))
	test.Scroll(u.Window.Canvas(), position(), 0, -48)
	if page.Offset.Y != bottom {
		t.Fatal("wheel did not leave the bottom of the text field", page.Offset)
	}
	inner.ScrollToOffset(fyne.NewPos(0, 10))
	test.Scroll(u.Window.Canvas(), position(), 0, 48)
	if inner.Offset.Y != 0 || page.Offset.Y != bottom-38 {
		t.Fatal("remaining wheel distance was lost at the top boundary", inner.Offset, page.Offset)
	}
	// Reversing direction after reaching the page bottom must not require a drag.
	page.ScrollToBottom()
	test.Scroll(u.Window.Canvas(), position(), 0, 48)
	if page.Offset.Y >= bottom {
		t.Fatal("cannot reverse scrolling at page bottom")
	}
}

func TestEntryWheelPreservesEditingAndHorizontalScroll(t *testing.T) {
	u := testWindow(t)
	u.Show()
	tabs := desktopTabs(t, u)
	tabs.SelectIndex(3)
	page := tabs.Items[3].Content.(*container.Scroll)
	entry := u.timeout
	entry.SetText(strings.Repeat("1234567890", 60))
	inner := entryScroll(t, entry)
	page.ScrollToBottom()
	inner.ScrollToTop()
	pos := u.App.Driver().AbsolutePositionForObject(entry).Add(fyne.NewPos(20, 20))
	before := page.Offset.Y
	test.Scroll(u.Window.Canvas(), pos, -48, 0)
	if inner.Offset.X <= 0 || page.Offset.Y != before {
		t.Fatal("horizontal scrolling was lost", inner.Offset, page.Offset)
	}
	x := inner.Offset.X
	test.Scroll(u.Window.Canvas(), pos, 0, 32)
	if page.Offset.Y >= before || inner.Offset.X != x {
		t.Fatal("vertical scrolling over single-line entry must move only the page")
	}
	entry.SetText("3000")
	pos = u.App.Driver().AbsolutePositionForObject(entry).Add(fyne.NewPos(20, 20))
	// Entry takes focus on MouseDown; Fyne's TapCanvas helper sends only Tapped.
	entry.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{Position: fyne.NewPos(20, 20), AbsolutePosition: pos}})
	test.TapCanvas(u.Window.Canvas(), pos)
	if u.Window.Canvas().Focused() == nil {
		t.Fatal("wheel receiver blocked entry focus")
	}
	u.Window.Canvas().Focused().TypedRune('9')
	if !strings.Contains(entry.Text, "9") {
		t.Fatal("wheel receiver blocked typing")
	}
	entry.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{Position: fyne.NewPos(20, 20), AbsolutePosition: pos}})
	test.Drag(u.Window.Canvas(), pos.Add(fyne.NewPos(40, 0)), 40, 0)
	if entry.SelectedText() == "" {
		t.Fatal("wheel receiver blocked text selection")
	}
}

func BenchmarkUnchangedStatusRefresh(b *testing.B) {
	a := test.NewApp()
	b.Cleanup(a.Quit)
	u := New(a, fakeAPI{}, context.Background())
	u.Window.Resize(fyne.NewSize(1080, 760))
	s := exampleStatus()
	u.display(s)
	u.Show()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.display(s)
	}
}
