package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/user"
	"time"

	"github.com/ably/ably-go/ably"
	"github.com/jroimartin/gocui"
)

type State struct {
	FileName string
	LastSend time.Time
}

type Logger struct{}

func (l *Logger) Printf(level ably.LogLevel, format string, v ...interface{}) {
	//fmt.Printf("          [%s] %s\n", level, fmt.Sprintf(format, v...))
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	var realtime *ably.Realtime
	var presense []*ably.PresenceMessage
	var gui *gocui.Gui
	var code string
	var file []byte
	var edit *Editor
	ctx := context.Background()

	rand.Seed(time.Now().UnixNano())

	args := Arguments{}
	err := args.ParseArgs()
	if err != nil {
		return err
	}

	if args.Help {
		Help()
		return nil
	}

	realtime, err = newRealtime()
	if err != nil {
		return err
	}
	defer realtime.Close()

	if args.Join || args.Cat {
		code = args.Arg
	} else {
		code = makeTag()
	}

	channel := realtime.Channels.Get(code)
	err = channel.Attach(ctx)
	if err != nil {
		return err
	}

	presense, err = channel.Presence.Get(ctx)
	if err != nil {
		return err
	}

	if (args.Join || args.Cat) && len(presense) == 0 {
		return errors.New(fmt.Sprintf("session '%s' does not exist", code))
	} else if !(args.Join || args.Cat) && len(presense) != 0 {
		return errors.New(fmt.Sprintf("session '%s' already exists", code))
	}

	layout := &Layout{Code: code, Cursors: make(map[string]Cursor, 0), Id: realtime.Auth.ClientID()}

	if !(args.Join || args.Cat) && args.Arg != "" {
		file, err = os.ReadFile(args.Arg)
		if err != nil {
			return err
		}
		layout.FileName = args.Arg
	}

	if args.Cat {
		return cat(ctx, channel)
	}

	gui, err = initGui()
	if err != nil {
		return err
	}

	gui.SetManager(layout)
	layout.Layout(gui)
	edit, err = MakeEditor(ctx, file, !args.Join, channel, gui, layout)
	if err != nil {
		return err
	}
	layout.Editor = edit

	_, err = channel.Presence.SubscribeAll(ctx, func(msg *ably.PresenceMessage) {
		presense, err := channel.Presence.Get(ctx)
		if err == nil {
			layout.Members = presense
			gui.Update(func(gui *gocui.Gui) error { return nil })
		}
	})
	if err != nil {
		return err
	}

	channel.SubscribeAll(ctx, func(msg *ably.Message) {
		gui.Update(func(gui *gocui.Gui) error {
			log, _ := gui.View("log")
			fmt.Fprintln(log, msg)
			return nil
		})
	})
	_, err = channel.Subscribe(ctx, "cursor", func(msg *ably.Message) {
		var cursor Cursor
		err := json.Unmarshal([]byte(msg.Data.(string)), &cursor)
		if err != nil {
			return
		}
		layout.Cursors[msg.ClientID] = cursor
		gui.Update(func(gui *gocui.Gui) error { return nil })
	})
	if err != nil {
		return err
	}

	user, err := user.Current()
	if err != nil {
		return err
	}

	name := user.Name
	if name == "" {
		name = user.Username
	}
	err = channel.Presence.Enter(ctx, name)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	err = gui.MainLoop()
	if err != nil && err != gocui.ErrQuit {
		return err
	}
	defer gui.Close()

	return nil
}

func newRealtime() (*ably.Realtime, error) {
	key, ok := os.LookupEnv("ABLY_KEY")
	if !ok {
		return nil, errors.New("ABLY_KEY not set")
	}
	return ably.NewRealtime(
		ably.WithKey(key),
		ably.WithClientID("editor-"+makeTag()),
		//ably.WithClientID(user.Username),
		ably.WithLogHandler(&Logger{}),
		//ably.WithLogLevel(ably.LogDebug),
	)
}

func makeTag() string {
	bytes := make([]byte, 6)
	rand.Read(bytes)
	return base64.StdEncoding.EncodeToString(bytes)
}
