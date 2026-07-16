// Command tscloud-engine is the headless engine behind TransSped.app. It has no
// UI: each subcommand prints one JSON object to stdout. The SwiftUI app (and
// scripts) drive it.
//
//	tscloud-engine status
//	tscloud-engine setup --user <email|phone>
//	tscloud-engine uninstall
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"tscloud/internal/setup"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

type result struct {
	OK      bool          `json:"ok"`
	Message string        `json:"message,omitempty"`
	Error   string        `json:"error,omitempty"`
	Code    string        `json:"code,omitempty"`
	Status  *setup.Status `json:"status,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

func run(args []string, out io.Writer) int {
	if len(args) == 0 {
		writeJSON(out, result{Error: "usage: tscloud-engine status|setup|uninstall", Code: "unknown"})
		return 2
	}
	switch args[0] {
	case "status":
		writeJSON(out, setup.GetStatus())
		return 0
	case "setup":
		fs := flag.NewFlagSet("setup", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		user := fs.String("user", "", "Trans Sped userID (email or phone)")
		if err := fs.Parse(args[1:]); err != nil {
			return emitErr(out, err)
		}
		st, err := setup.Run(*user)
		if err != nil {
			return emitErr(out, err)
		}
		writeJSON(out, result{OK: true, Message: "Setup complete.", Status: st})
		return 0
	case "uninstall":
		notes, err := setup.Uninstall()
		if err != nil {
			return emitErr(out, err)
		}
		writeJSON(out, result{OK: true, Message: "Uninstall complete.", Notes: notes})
		return 0
	default:
		writeJSON(out, result{Error: "unknown command: " + args[0], Code: "unknown"})
		return 2
	}
}

func emitErr(out io.Writer, err error) int {
	r := result{Error: err.Error(), Code: "unknown"}
	var ce *setup.CodedError
	if errors.As(err, &ce) {
		r.Code = ce.Code
		r.Error = ce.Message
	}
	writeJSON(out, r)
	return 1
}

func writeJSON(out io.Writer, v any) {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
