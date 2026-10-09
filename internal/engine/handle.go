package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
)

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return errors.New("missing arguments")
	}
	return json.Unmarshal(raw, v)
}

// Handle implements ipc.Handler.
func (e *Engine) Handle(ctx context.Context, cmd string, raw json.RawMessage) error {
	switch cmd {
	case ipc.CmdPing:
		return nil
	case ipc.CmdReloadDots:
		e.ReloadDots()
		e.ReloadConfig()
		return nil
	case ipc.CmdPauseAll:
		e.TogglePauseAll()
		return nil
	case ipc.CmdAttach:
		var a ipc.AttachArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Attach(ctx, a.PaneID, a.Slugs)
	case ipc.CmdDetach:
		var a ipc.DotArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Detach(a.PaneID, a.Slug)
	case ipc.CmdReset:
		var a ipc.DotArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Reset(a.PaneID, a.Slug)
	case ipc.CmdSend:
		var a ipc.SendArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.SendCards(a.IDs)
	case ipc.CmdDismiss:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Dismiss(a.ID)
	case ipc.CmdEdit:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Edit(a.ID, a.Body)
	case ipc.CmdSummon:
		var a ipc.SummonArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Summon(ctx, a.PaneID, a.Slug, a.Question)
	case ipc.CmdSetMode:
		var a ipc.SetModeArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.SetMode(a.Slug, dots.Mode(a.Mode))
	case ipc.CmdPause:
		var a ipc.PauseArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.SetPaused(a.PaneID, a.Slug, a.Paused)
	case ipc.CmdRemember:
		var a ipc.RememberArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Remember(a.CardID, memory.RememberAs(a.As), a.Reason)
	case ipc.CmdMemoryChanged:
		e.MemoryChanged()
		return nil
	case ipc.CmdDraft:
		var a ipc.DraftArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		_, err := e.Draft(ctx, a.PaneID, a.Slug, a.Instruction, a.FromCardID)
		return err
	case ipc.CmdApplyPatch:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.ApplyPatch(a.ID, a.SHA256) // an empty hash is refused
	case ipc.CmdDiscardPatch:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.DiscardPatch(a.ID)
	case ipc.CmdCouncil:
		var a ipc.CouncilArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		_, err := e.Council(ctx, a.PaneID, a.Question, a.Slugs)
		return err
	case ipc.CmdCouncilPreflight:
		var a ipc.CouncilArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		_, err := e.councilPreflight(a.PaneID, a.Request, a.Slugs)
		return err
	}
	return fmt.Errorf("unknown command %q", cmd)
}
