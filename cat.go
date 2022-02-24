package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ably/ably-go/ably"
)

func cat(ctx context.Context, channel *ably.RealtimeChannel) error {
	var text [][]byte = [][]byte{{}}
	_history := channel.History(ably.HistoryWithDirection(ably.Forwards))
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

	text = handleCatMessage(text, item)

	for {
		ok = history.Next(ctx)

		if !ok {
			break
		}

		item = history.Item()
		text = handleCatMessage(text, item)
	}

	err = history.Err()
	if err != nil {
		return err
	}

	for _, line := range text {
		fmt.Println(string(line))
	}

	return nil
}

func handleCatMessage(text [][]byte, msg *ably.Message) [][]byte {
	switch msg.Name {
	case "new":
		msg := msg.Data.(string)
		text = applyNew(msg, text)
	case "add":
		var add Add
		data := msg.Data.(string)
		err := json.Unmarshal([]byte(data), &add)
		if err != nil {
			break
		}
		text = applyAdd(add, text)
	case "delete":
		var del Delete
		data := msg.Data.(string)
		err := json.Unmarshal([]byte(data), &del)
		if err != nil {
			break
		}
		text = applyDel(del, text)
	}
	return text
}
