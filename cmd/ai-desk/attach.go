package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/falluba/ai-desk/pkg/deck"
)

func sessionAttachCommand(target string) (*exec.Cmd, error) {
	if target != "" {
		sdb, err := deck.OpenStateDB("")
		if err == nil {
			defer sdb.Close()
			instances, err := sdb.LoadInstances()
			if err != nil {
				return nil, err
			}
			for _, inst := range instances {
				if target == inst.ID || target == inst.Title || target == inst.TmuxSession {
					return sdb.PrepareSessionAttach(nil, inst.ID)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("open session database: %w", err)
		}
	}
	return deck.TmuxAttachCommand(target, ""), nil
}

func attachSession(target string) error {
	cmd, err := sessionAttachCommand(target)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
