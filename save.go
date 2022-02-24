package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/jroimartin/gocui"
)

type Save struct {
	Editor *Editor
	Force  bool
}

func (s *Save) Layout(gui *gocui.Gui) error {
	maxX, maxY := gui.Size()
	var input, label *gocui.View

	_, err := gui.SetView("save-box", maxX/2-33, maxY/2-3, maxX/2+33, maxY/2+3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
	}

	label, err = gui.SetView("save-label", maxX/2-30, maxY/2-2, maxX/2+30, maxY/2)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		fmt.Fprint(label, "Enter Filename:")
		label.Frame = false

	}

	input, err = gui.SetView("save-input", maxX/2-30, maxY/2, maxX/2+30, maxY/2+2)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		fmt.Fprint(input, s.Editor.Layout.FileName)
		input.SetCursor(len(s.Editor.Layout.FileName), 0)
	}
	input.Editor = s
	input.Editable = true

	gui.SetCurrentView("save-input")
	return nil
}

func (s *Save) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	switch {
	case key == gocui.KeyEnter:
		s.Editor.Layout.FileName = strings.TrimSpace(v.Buffer())
		err := s.Save()
		if err != nil {
			label, _ := s.Editor.Gui.View("save-label")
			label.Clear()
			fmt.Fprint(label, err)
			return
		}
		s.Editor.Gui.DeleteView("save-box")
		s.Editor.Gui.DeleteView("save-input")
		s.Editor.Gui.DeleteView("save-label")
		s.Editor.Layout.Save = nil
	default:
		gocui.DefaultEditor.Edit(v, key, ch, mod)
	}
}

func (s *Save) Save() error {
	err := os.WriteFile(s.Editor.Layout.FileName, bytes.Join(s.Editor.Text, []byte{'\n'}), 0644)
	if err != nil {
		s.Editor.Nodify(err.Error())
	} else {
		s.Editor.Nodify("Saved")
	}
	return err
}
