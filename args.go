package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Arguments struct {
	Arg  string
	Join bool
	Help bool
	Cat  bool
}

func usage() {
	fmt.Println(`usage:
	sync-edit --join code
	sync-edit path/to/file`)
}

func (a *Arguments) ParseArgs() error {
	var name *string

	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") {
			switch arg {
			case "--join", "-j":
				a.Join = true
			case "--help", "-h":
				a.Help = true
			case "--cat", "-c":
				a.Cat = true
			default:
				return errors.New(fmt.Sprintf("unkown argument %s", arg))
			}
		} else if name == nil {
			name = &arg
			a.Arg = arg
		} else {
			usage()
			os.Exit(1)
		}
	}

	if name == nil && (a.Join || a.Cat) {
		usage()
		os.Exit(1)
	}

	return nil
}

func Help() {
	fmt.Println(
		`usage:
    sync-edit --join <session code>
    sync-edit [path/to/file]

    Edit files collaboratively

    The ABLY_KEY environment variable must be set to your API key

    -j, --join             Join a session instead of creating one
    -c, --cat              Print the contents of a session
    -h. --help             Display this help menu`)
}
