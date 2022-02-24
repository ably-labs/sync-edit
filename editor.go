package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ably/ably-go/ably"
	"github.com/jroimartin/gocui"
)

// Theory crafting, dealing with conflicts
//
// There will be three states:
// - The Canonical text
// - The text the user sees
// - Diffs between them
//
// The Canonical text is only updated via ably messages. This allows us to keep a consistent
// order by relying on ably's order gaurintees.
//
// However having the text have to go through the network and back to be displayed would be
// very unerganomic. So when an edit is made, we display it instnatly and record it as a diff
// from the canonical text. When we get the edit back from ably we delete the diff, update the
// canonical text, then diaplay the cononical text with remaining diffs applied.

type Editor struct {
	Layout     *Layout
	EditBuffer interface{}
	LastCursor Cursor
	EditMux    sync.Mutex
	Text       [][]byte
	Channel    *ably.RealtimeChannel
	Gui        *gocui.Gui
	Cursors    map[string]gocui.View
	Quit       chan struct{}
	Queue      chan interface{}
}

type Add struct {
	Line int    `json:"line"`
	Pos  int    `json:"pos"`
	Text string `json:"text"`
}

type Delete struct {
	Line  int `json:"line"`
	Pos   int `json:"pos"`
	Count int `json:"count"`
}

func MakeEditor(ctx context.Context, text []byte, owner bool, channel *ably.RealtimeChannel, gui *gocui.Gui, layout *Layout) (*Editor, error) {
	edit := &Editor{Channel: channel, Gui: gui, Layout: layout}

	_, err := channel.SubscribeAll(ctx, func(msg *ably.Message) {
		edit.handleMessage(msg)
	})

	edit.Text = [][]byte{{}}

	if owner {
		err = edit.Channel.Publish(context.Background(), "new", string(text))
		if err != nil {
			return nil, err
		}
		edit.Text = applyNew(string(text), edit.Text)
		edit.Layout.Editable = true
	} else {
		edit.initFromHistory(ctx)
	}

	go edit.publishQueue()
	go edit.editLoop()

	/*gui.SetKeybinding("", gocui.KeyCtrlSpace, gocui.ModNone, func(gui *gocui.Gui, view *gocui.View) error {
		log, _ := gui.View("log")
		fmt.Fprintln(log, edit.Text)
		return nil
	})*/

	return edit, err
}

func (e *Editor) publishQueue() {
	e.Queue = make(chan interface{}, 100)
	buffer := make([]*ably.Message, 0)
	ctx := context.Background()

	buffChange := func(msg interface{}) {
		switch edit := msg.(type) {
		case *Add:
			// work around ably bug
			js, _ := json.Marshal(edit)
			buffer = append(buffer, &ably.Message{Name: "add", Data: js})
		case *Delete:
			// work around ably bug
			js, _ := json.Marshal(edit)
			buffer = append(buffer, &ably.Message{Name: "delete", Data: js})
		case *Cursor:
			// work around ably bug
			js, _ := json.Marshal(edit)
			buffer = append(buffer, &ably.Message{Name: "cursor", Data: js})
		}
	}

	for {
		buffChange(<-e.Queue)

	f:
		for {
			select {
			case msg := <-e.Queue:
				buffChange(msg)
			default:
				break f
			}
		}

		err := e.Channel.PublishMultiple(ctx, buffer)
		if err != nil {
			e.Nodify(err.Error())
		}
		buffer = nil
	}
}

func applyAdd(add Add, text [][]byte) [][]byte {
	if add.Line < 0 || add.Line >= len(text) || add.Pos < 0 || add.Pos > len(text[add.Line]) {
		return text
	}

	if add.Text == "" {
		text = append(text[:add.Line+1], text[add.Line:]...)
		text[add.Line+1] = text[add.Line][add.Pos:]
		text[add.Line] = text[add.Line][:add.Pos]
	} else if add.Pos == len(text[add.Line]) {
		text[add.Line] = append(text[add.Line], []byte(add.Text)...)
	} else {
		end := append([]byte(nil), text[add.Line][add.Pos:]...)
		text[add.Line] = append(text[add.Line][:add.Pos], []byte(add.Text)...)
		text[add.Line] = append(text[add.Line], end...)
	}

	return text
}

func applyNew(s string, text [][]byte) [][]byte {
	return bytes.Split([]byte(s), []byte{'\n'})
}

func applyDel(del Delete, text [][]byte) [][]byte {
	if del.Line < 0 || del.Line >= len(text) || del.Pos < 0 || del.Count < 0 {
		return text
	}
	if del.Count != 0 && del.Count+del.Pos-1 >= len(text[del.Line]) {
		return text
	}

	if del.Count == 0 {
		line := text[del.Line]
		text = append(text[:del.Line], text[del.Line+1:]...)
		text[del.Line] = append(line, text[del.Line]...)
	} else {
		text[del.Line] = append(text[del.Line][:del.Pos], text[del.Line][del.Pos+del.Count:]...)
	}
	return text

}

func (e *Editor) handleMessage(msg *ably.Message) {
	e.EditMux.Lock()
	defer e.EditMux.Unlock()
	switch msg.Name {
	case "new":
		text := msg.Data.(string)
		e.Layout.Editable = true
		e.EditBuffer = nil
		e.Text = applyNew(text, e.Text)
		e.Layout.Redraw = true
		e.View().SetCursor(0, 0)
	case "add":
		var add Add
		data := msg.Data.(string)
		err := json.Unmarshal([]byte(data), &add)
		if err != nil {
			break
		}
		if msg.ClientID != e.Layout.Id {
			x, y := e.cursorPos()
			xo, yo := e.View().Origin()
			if add.Text == "" && (add.Line < y || add.Line == y && add.Pos >= x) {
				e.View().SetCursor(0, y-yo+1)
			} else if add.Text != "" && add.Line == y && add.Pos+len(add.Text) >= x {
				e.View().SetCursor(x-xo+len(add.Text), y-yo)
			}
		}
		e.Text = applyAdd(add, e.Text)
		e.Layout.Redraw = true
	case "delete":
		var del Delete
		data := msg.Data.(string)
		err := json.Unmarshal([]byte(data), &del)
		if err != nil {
			break
		}

		if del.Line < 0 {
			break
		}

		if msg.ClientID != e.Layout.Id {
			x, y := e.cursorPos()
			xo, yo := e.View().Origin()
			if del.Count == 0 && del.Line+1 == y {
				e.View().SetCursor(len(e.Text[y-1])+x-xo, y-yo-1)
			} else if del.Count == 0 && del.Line+1 <= y {
				e.View().SetCursor(x-xo, y-yo-1)
			} else if del.Line == y && del.Pos+del.Count-1 < x {
				e.View().SetCursor(x-xo-del.Count, y-yo)
			}
		}
		e.Text = applyDel(del, e.Text)
		e.Layout.Redraw = true
	}
}

func (e *Editor) View() *gocui.View {
	v, _ := e.Gui.View("editor")
	return v
}

func (e *Editor) Log() *gocui.View {
	v, _ := e.Gui.View("log")
	return v
}

func (e *Editor) Nodify(s string) {
	e.Gui.Update(func(gui *gocui.Gui) error {
		notify, err := gui.View("notify")
		if err == nil {
			notify.Clear()
			fmt.Fprint(notify, s)
		}
		return nil
	})
}

func (e *Editor) initFromHistory(ctx context.Context) error {
	_history := e.Channel.History(ably.HistoryWithDirection(ably.Forwards))
	history, err := _history.Items(ctx)
	if err != nil {
		return err
	}

	ok := history.Next(ctx)
	if !ok {
		return errors.New("No file found in session")
	}

	item := history.Item()

	if item.Name != "new" {
		return errors.New("No file found in session")
	}

	e.handleMessage(item)

	for {
		ok = history.Next(ctx)

		if !ok {
			break
		}

		item = history.Item()
		e.handleMessage(item)
	}

	return history.Err()

}

func (e *Editor) flushChanges(cursor bool) {
	if e.EditBuffer != nil {
		e.Queue <- e.EditBuffer
		e.EditBuffer = nil
	}

	v, err := e.Gui.View("editor")
	if cursor && err == nil {
		x, y := v.Cursor()
		xo, yo := v.Origin()
		cur := Cursor{X: x + xo, Y: y + yo}
		if e.LastCursor != cur {
			e.LastCursor = cur
			e.Queue <- &cur
		}
	}
}

func (e *Editor) editLoop() {
	for {
		select {
		case <-time.After(600000 * time.Microsecond):
			e.EditMux.Lock()
			e.flushChanges(true)
			e.EditMux.Unlock()
		}
	}
}

func (e *Editor) cursorPos() (int, int) {
	ox, oy := e.View().Origin()
	x, y := e.View().Cursor()
	return x + ox, y + oy
}

func (e *Editor) AddChar(ch rune) {
	add, ok := e.EditBuffer.(*Add)
	if !ok {
		e.flushChanges(true)
		x, y := e.cursorPos()
		e.EditBuffer = &Add{Line: y, Pos: x, Text: string(ch)}
	} else {
		add.Text += string(ch)
	}
}
func (e *Editor) DelChar(before bool) {
	del, ok := e.EditBuffer.(*Delete)
	x, y := e.cursorPos()

	if y < 0 {
		return
	}

	if !ok || (x == 0 && before) {
		e.flushChanges(true)
		del = &Delete{Line: y, Pos: x, Count: 0}
		e.EditBuffer = del
	}
	if x == 0 && before {
		del.Line -= 1
		e.flushChanges(true)
	} else {
		del.Count += 1
		if before {
			del.Pos -= 1
		}
	}
}

func (e *Editor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	e.EditMux.Lock()
	defer e.EditMux.Unlock()
	switch {
	case ch != 0 && mod == 0:
		e.AddChar(ch)
		v.EditWrite(ch)
	case key == gocui.KeySpace:
		e.AddChar(' ')
		v.EditWrite(' ')

		add, ok := e.EditBuffer.(*Add)
		if ok && add.Text != " " && !strings.HasSuffix(add.Text, "  ") {
			e.flushChanges(true)
		}
	case key == gocui.KeyBackspace || key == gocui.KeyBackspace2:
		e.DelChar(true)
		v.EditDelete(true)
	case key == gocui.KeyDelete:
		e.DelChar(false)
		v.EditDelete(false)
	//case key == gocui.KeyInsert:
	//	v.Overwrite = !v.Overwrite
	case key == gocui.KeyEnter:
		x, y := e.cursorPos()
		v.EditNewLine()
		e.flushChanges(true)
		e.EditBuffer = &Add{Line: y, Pos: x, Text: ""}
		e.flushChanges(true)
	case key == gocui.KeyArrowDown:
		_, y := e.cursorPos()
		if y+1 < len(e.Text) {
			e.flushChanges(false)
			v.MoveCursor(0, 1, false)
		} else {
			_, ry := e.View().Cursor()
			v.SetCursor(len(e.Text[ry]), ry)
		}
	case key == gocui.KeyArrowUp:
		e.flushChanges(false)
		v.MoveCursor(0, -1, false)
	case key == gocui.KeyArrowLeft:
		e.flushChanges(false)
		v.MoveCursor(-1, 0, false)
	case key == gocui.KeyArrowRight:
		x, y := e.cursorPos()
		if y+1 < len(e.Text) || x < len(e.Text[y]) {
			e.flushChanges(false)
			v.MoveCursor(1, 0, false)
		}
	}
}

func (e *Editor) dupText() [][]byte {
	text := make([][]byte, len(e.Text))

	for i := range e.Text {
		text[i] = append([]byte(nil), e.Text[i]...)
	}

	return text
}

func (e *Editor) displyText() {
	e.View().Clear()

	text := e.dupText()
	switch msg := e.EditBuffer.(type) {
	case *Add:
		text = applyAdd(*msg, text)
	case *Delete:
		text = applyDel(*msg, text)
	}

	// Hack for bug in gocui
	if len(e.Text[0]) == 0 {
		e.View().Write([]byte{' '})
	}
	for _, line := range text {
		e.View().Write(line)
		e.View().Write([]byte{'\n'})
	}
}
