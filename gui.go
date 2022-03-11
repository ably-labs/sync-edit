package main

import (
	"context"
	"fmt"

	"github.com/ably/ably-go/ably"
	"github.com/jroimartin/gocui"
)

var colours []gocui.Attribute = []gocui.Attribute{gocui.ColorBlue, gocui.ColorCyan, gocui.ColorGreen, gocui.ColorMagenta, gocui.ColorRed}

type Layout struct {
	Id       string
	FileName string
	Log      bool
	Editable bool
	Setup    bool
	Save     *Save
	Redraw   bool
	Editor   *Editor
	Code     string
	Members  []*ably.PresenceMessage
	Cursors  map[string]Cursor
}

type Cursor struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func updateBar(gui *gocui.Gui, code string, users int) {
	bar, err := gui.View("bar")
	if err == nil {
		bar.Clear()
		fmt.Fprintf(bar, "Users: %d Session: %s", users, code)
	}
}

func initGui() (*gocui.Gui, error) {
	gui, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return nil, err
	}
	return gui, nil
}

func (l *Layout) Layout(gui *gocui.Gui) error {
	var notify, editor, bar, keys, log, members *gocui.View
	var err error

	//gui.Mouse = true
	gui.Cursor = true
	maxX, maxY := gui.Size()

	log, err = gui.SetView("log", maxX/2, maxY/2, maxX-1, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		log.Autoscroll = true
	}

	editor, err = gui.SetView("editor", 0, 0, maxX-21, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		//editor.Autoscroll = true
		//editor.Wrap = true
	}
	if l.FileName != "" {
		editor.Title = l.FileName
	} else {
		editor.Title = "Unsaved"
	}
	editor.Editable = l.Editable
	editor.Editor = l.Editor

	_, err = gui.SetCurrentView("editor")
	if err != nil {
		return err
	}

	notify, err = gui.SetView("notify", 0, maxY-3, maxX, maxY-1)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		notify.Frame = false
	}

	keys, err = gui.SetView("keys", maxX-42, maxY-2, maxX, maxY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		keys.Frame = false
		fmt.Fprint(keys, "C-x Exit  C-n New  C-s Save  C-a Save As")
	}

	members, err = gui.SetView("members", maxX-20, 0, maxX-1, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		members.Frame = true
	}
	members.Clear()
	for i, user := range l.Members {
		col := colours[i%len(colours)]
		fmt.Fprintf(members, "\x1b[0;%dm%s\n", col+29, user.Message.Data.(string))
	}

	bar, err = gui.SetView("bar", 0, maxY-2, maxX-42, maxY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		bar.Frame = false
	}
	bar.Clear()
	fmt.Fprintf(bar, "Users: %d Session: %s", len(l.Members), l.Code)

	if !l.Setup {
		l.Setup = true
		err = gui.SetKeybinding("", gocui.KeyCtrlX, gocui.ModNone, quit)
		if err != nil {
			return err
		}
		err = gui.SetKeybinding("", gocui.KeyCtrlL, gocui.ModNone, func(gui *gocui.Gui, v *gocui.View) error {
			if l.Log {
				gui.SetViewOnBottom("log")
			} else {
				gui.SetViewOnTop("log")
			}
			l.Log = !l.Log
			return nil
		})
		if err != nil {
			return err
		}
		err = gui.SetKeybinding("editor", gocui.KeyCtrlA, gocui.ModNone, func(gui *gocui.Gui, v *gocui.View) error {
			l.Save = &Save{Editor: l.Editor, Force: true}
			return nil
		})
		if err != nil {
			return err
		}
		err = gui.SetKeybinding("editor", gocui.KeyCtrlS, gocui.ModNone, func(gui *gocui.Gui, v *gocui.View) error {
			l.Save = &Save{Editor: l.Editor}
			return nil
		})
		if err != nil {
			return err
		}
		err = gui.SetKeybinding("editor", gocui.KeyCtrlN, gocui.ModNone, func(gui *gocui.Gui, v *gocui.View) error {
			err = l.Editor.Channel.Publish(context.Background(), "new", "")
			if err == nil {
				editor.Editable = false
			} else {
				l.Editor.Nodify(err.Error())
			}

			return nil
		})
	}

	for i, member := range l.Members {
		if member.ClientID == l.Id {
			continue
		}

		pos, ok := l.Cursors[member.ClientID]
		if !ok {
			continue
		}

		xs, ys := editor.Size()
		xo, yo := editor.Origin()
		x := pos.X - xo + 1
		y := pos.Y - yo + 1

		if x < 1 || x > xs+1 || y < 1 || y > ys+1 {
			gui.DeleteView("cursor-" + member.ClientID)
		} else {
			view, err := gui.SetView("cursor-"+member.ClientID, x-1, y-1, x+1, y+1)
			if err != nil {
				if err != gocui.ErrUnknownView {
					return err
				}
				view.Frame = false
			}
			view.BgColor = colours[i%len(colours)]
			view.Clear()
			lines := editor.BufferLines()
			fmt.Fprintln(log, lines)
			fmt.Fprintln(log, pos.X, pos.Y, len(lines), len(lines[pos.Y]))
			if len(lines) > pos.Y && len(lines[pos.Y]) > pos.X {
				view.Write([]byte{lines[pos.Y][pos.X]})
			}
		}
	}

	if l.Redraw {
		l.Redraw = false
		l.Editor.displyText()
	}

	if l.Save != nil {
		if l.FileName == "" || l.Save.Force {
			err = l.Save.Layout(l.Editor.Gui)
			if err != nil {
				return err
			}
		} else {
			l.Save.Save()
		}
	}

	//gui.SetViewOnTop("log")

	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}
