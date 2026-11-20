//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// formEntry passes unused vertical wheel movement to its containing page.
// Fyne sends a wheel event only to the deepest Scrollable, including an Entry's
// internal scroll container even when that container cannot move.
type formEntry struct {
	widget.Entry
	page *container.Scroll
}

func newFormEntry(multiline bool) *formEntry {
	e := &formEntry{}
	e.MultiLine = multiline
	e.Wrapping = fyne.TextWrap(fyne.TextTruncateClip)
	e.ExtendBaseWidget(e)
	if multiline {
		e.Wrapping = fyne.TextWrapWord
		e.SetMinRowsVisible(3)
	}
	return e
}

func newFormScroll(content fyne.CanvasObject, entries ...*formEntry) *container.Scroll {
	page := container.NewVScroll(content)
	for _, entry := range entries {
		entry.page = page
	}
	return page
}

func (e *formEntry) CreateRenderer() fyne.WidgetRenderer {
	r := e.Entry.CreateRenderer()
	var inner *container.Scroll
	for _, obj := range r.Objects() {
		if scroll, ok := obj.(*container.Scroll); ok {
			inner = scroll
			break
		}
	}
	wheel := &wheelReceiver{onScroll: func(event *fyne.ScrollEvent) {
		remaining := *event
		remaining.Scrolled.DX = 0
		if inner != nil {
			local := *event
			// Vertical wheel motion over a single-line field belongs to the page.
			// Keep horizontal touchpad motion available for long field values.
			if !e.MultiLine && e.page != nil {
				local.Scrolled.DY = 0
			}
			before := inner.Offset.Y
			if local.Scrolled.DX != 0 || local.Scrolled.DY != 0 {
				inner.Scrolled(&local)
			}
			remaining.Scrolled.DY -= before - inner.Offset.Y
		}
		if e.page != nil && remaining.Scrolled.DY != 0 {
			e.page.Scrolled(&remaining)
		}
	}}
	wheel.ExtendBaseWidget(wheel)
	return &formEntryRenderer{WidgetRenderer: r, wheel: wheel}
}

// This empty widget participates only in wheel hit testing. Taps, selection,
// focus, keyboard input and scrollbar dragging still reach the original Entry.
type wheelReceiver struct {
	widget.BaseWidget
	onScroll func(*fyne.ScrollEvent)
}

func (w *wheelReceiver) Scrolled(event *fyne.ScrollEvent) { w.onScroll(event) }
func (w *wheelReceiver) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewWithoutLayout())
}

type formEntryRenderer struct {
	fyne.WidgetRenderer
	wheel   *wheelReceiver
	objects []fyne.CanvasObject
}

func (r *formEntryRenderer) Layout(size fyne.Size) {
	r.WidgetRenderer.Layout(size)
	r.wheel.Resize(size)
}

func (r *formEntryRenderer) Objects() []fyne.CanvasObject {
	objects := r.WidgetRenderer.Objects()
	if len(r.objects) != len(objects)+1 {
		r.objects = make([]fyne.CanvasObject, len(objects)+1)
	}
	copy(r.objects, objects)
	r.objects[len(objects)] = r.wheel
	return r.objects
}
